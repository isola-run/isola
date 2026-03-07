# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator

import httpx
import pytest

from isola import APIConnectionError, IsolaError, NotFoundError
from isola._streaming import MAX_RECONNECTS, AsyncStreamReader, StreamReader


def _sse_event(data: str, event_id: int) -> bytes:
    """Format a single SSE data event as wire bytes."""
    lines = [f"data: {line}" for line in data.split("\n")]
    lines.append(f"id: {event_id}")
    lines.append("")  # blank line terminates event
    return ("\n".join(lines) + "\n").encode("utf-8")


class _FakeSyncResponse:
    def __init__(self, chunks: list[bytes], *, raise_after: Exception | None = None, status_code: int = 200) -> None:
        self.status_code = status_code
        self._chunks = chunks
        self._raise_after = raise_after

    def iter_bytes(self) -> Iterator[bytes]:
        yield from self._chunks
        if self._raise_after is not None:
            raise self._raise_after

    def read(self) -> bytes:
        return b""


class _FakeSyncCM:
    def __init__(self, response: _FakeSyncResponse | None = None, *, enter_exc: Exception | None = None) -> None:
        self._response = response
        self._enter_exc = enter_exc

    def __enter__(self) -> _FakeSyncResponse:
        if self._enter_exc is not None:
            raise self._enter_exc
        assert self._response is not None
        return self._response

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        return None


class _FakeSyncAPI:
    def __init__(self, sequence: list[_FakeSyncCM]) -> None:
        self._sequence = sequence
        self.calls: list[int] = []

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, int] | None = None,
        headers: dict[str, str] | None = None,
        timeout: object = None,
    ) -> _FakeSyncCM:
        del path, params, timeout
        offset = int(headers["Last-Event-ID"]) if headers and "Last-Event-ID" in headers else 0
        self.calls.append(offset)
        return self._sequence.pop(0)


class _FakeAsyncResponse:
    def __init__(self, chunks: list[bytes], *, raise_after: Exception | None = None, status_code: int = 200) -> None:
        self.status_code = status_code
        self._chunks = chunks
        self._raise_after = raise_after

    async def aiter_bytes(self) -> AsyncIterator[bytes]:
        for chunk in self._chunks:
            yield chunk
        if self._raise_after is not None:
            raise self._raise_after

    async def aread(self) -> bytes:
        return b""


class _FakeAsyncCM:
    def __init__(self, response: _FakeAsyncResponse | None = None, *, enter_exc: Exception | None = None) -> None:
        self._response = response
        self._enter_exc = enter_exc

    async def __aenter__(self) -> _FakeAsyncResponse:
        if self._enter_exc is not None:
            raise self._enter_exc
        assert self._response is not None
        return self._response

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        return None


class _FakeAsyncAPI:
    def __init__(self, sequence: list[_FakeAsyncCM]) -> None:
        self._sequence = sequence
        self.calls: list[int] = []

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, int] | None = None,
        headers: dict[str, str] | None = None,
        timeout: object = None,
    ) -> _FakeAsyncCM:
        del path, params, timeout
        offset = int(headers["Last-Event-ID"]) if headers and "Last-Event-ID" in headers else 0
        self.calls.append(offset)
        return self._sequence.pop(0)


# --- Reconnect tests ---


def test_sync_stream_reconnects_and_resumes_offset(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=httpx.ConnectError("disconnect"))),
            _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    output = "".join(stream)

    assert output == "abcd"
    assert api.calls == [0, 2]


@pytest.mark.parametrize(
    "error",
    [
        httpx.ConnectError("connection refused"),
        httpx.ReadError("server dropped"),
        httpx.WriteError("broken pipe"),
        httpx.CloseError("close failed"),
        httpx.ConnectTimeout("connect timed out"),
        httpx.ProxyError("proxy down"),
    ],
    ids=["ConnectError", "ReadError", "WriteError", "CloseError", "ConnectTimeout", "ProxyError"],
)
def test_sync_stream_reconnects_on_network_error(monkeypatch: pytest.MonkeyPatch, error: Exception) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=error)),
            _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    output = "".join(stream)

    assert output == "abcd"
    assert api.calls == [0, 2]


@pytest.mark.asyncio
async def test_async_stream_reconnects_and_resumes_offset(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("a", 1)], raise_after=httpx.ConnectError("disconnect"))),
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("b", 2), _sse_event("c", 3)])),
        ]
    )

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    chunks_list: list[str] = []
    async for chunk in stream:
        chunks_list.append(chunk)

    assert "".join(chunks_list) == "abc"
    assert api.calls == [0, 1]


def test_sync_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([_FakeSyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)])

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(APIConnectionError):
        list(stream)


def test_sync_stream_connect_error_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(enter_exc=httpx.ConnectError("refused")),
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    output = "".join(stream)

    assert output == "hello"
    assert api.calls == [0, 0]


def test_sync_stream_connect_timeout_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(enter_exc=httpx.ConnectTimeout("connect timed out")),
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    output = "".join(stream)

    assert output == "hello"
    assert api.calls == [0, 0]


def test_sync_stream_connect_timeout_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([_FakeSyncCM(enter_exc=httpx.ConnectTimeout("down")) for _ in range(MAX_RECONNECTS + 1)])

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(APIConnectionError):
        list(stream)


def test_sync_stream_http_error_propagates(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([], status_code=404)),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(NotFoundError):
        list(stream)


@pytest.mark.asyncio
async def test_async_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([_FakeAsyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)])

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(APIConnectionError):
        async for _ in stream:
            pass


@pytest.mark.asyncio
async def test_async_stream_connect_error_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(enter_exc=httpx.ConnectError("refused")),
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    chunks_list: list[str] = []
    async for chunk in stream:
        chunks_list.append(chunk)

    assert "".join(chunks_list) == "hello"
    assert api.calls == [0, 0]


@pytest.mark.asyncio
async def test_async_stream_connect_timeout_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(enter_exc=httpx.ConnectTimeout("connect timed out")),
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    chunks_list: list[str] = []
    async for chunk in stream:
        chunks_list.append(chunk)

    assert "".join(chunks_list) == "hello"
    assert api.calls == [0, 0]


@pytest.mark.asyncio
async def test_async_stream_connect_timeout_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([_FakeAsyncCM(enter_exc=httpx.ConnectTimeout("down")) for _ in range(MAX_RECONNECTS + 1)])

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(APIConnectionError):
        async for _ in stream:
            pass


@pytest.mark.asyncio
async def test_async_stream_http_error_propagates(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([], status_code=404)),
        ]
    )

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(NotFoundError):
        async for _ in stream:
            pass


# --- SSE parsing tests ---


def test_sync_stream_parses_multiline_sse_data() -> None:
    """Multiple data: lines in one event are joined with newlines."""
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"data: hello\ndata: world\nid: 11\n\n"])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "hello\nworld"


def test_sync_stream_filters_keepalive_comments() -> None:
    """SSE comments (: keepalive) are invisible to the parser."""
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(
                _FakeSyncResponse(
                    [
                        _sse_event("hello", 5),
                        b": keepalive\n\n",
                        _sse_event("world", 10),
                    ]
                )
            ),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "helloworld"


def test_sync_stream_preserves_trailing_newline() -> None:
    """Output ending with a newline round-trips through SSE correctly."""
    # "hello\n" → data: hello\ndata: \nid: 6\n\n
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"data: hello\ndata: \nid: 6\n\n"])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "hello\n"


@pytest.mark.asyncio
async def test_async_stream_parses_multiline_sse_data() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([b"data: hello\ndata: world\nid: 11\n\n"])),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    assert "".join([c async for c in stream]) == "hello\nworld"


@pytest.mark.asyncio
async def test_async_stream_filters_keepalive_comments() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(
                _FakeAsyncResponse(
                    [
                        _sse_event("hello", 5),
                        b": keepalive\n\n",
                        _sse_event("world", 10),
                    ]
                )
            ),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    assert "".join([c async for c in stream]) == "helloworld"


# --- UTF-8 defense-in-depth tests ---


def test_sync_stream_decodes_utf8() -> None:
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "hello world"


def test_sync_stream_handles_split_multibyte() -> None:
    # "café" in UTF-8: b"caf\xc3\xa9"
    # Split é (0xc3 0xa9) across two chunks within one SSE event
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"data: caf\xc3", b"\xa9\nid: 5\n\n"])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "caf\u00e9"


def test_sync_stream_handles_replacement_character() -> None:
    # Server replaces invalid UTF-8 with U+FFFD before sending SSE
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello\ufffdworld", 11)])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert "".join(stream) == "hello\ufffdworld"


@pytest.mark.asyncio
async def test_async_stream_decodes_utf8() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)])),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    assert "".join([c async for c in stream]) == "hello world"


@pytest.mark.asyncio
async def test_async_stream_handles_split_multibyte() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([b"data: caf\xc3", b"\xa9\nid: 5\n\n"])),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    assert "".join([c async for c in stream]) == "caf\u00e9"


# --- Single-use guard tests ---


def test_sync_stream_single_use_guard() -> None:
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("data", 4)])),
        ]
    )

    stream = StreamReader(api, "/path")
    list(stream)

    with pytest.raises(RuntimeError, match="single-use"):
        list(stream)


@pytest.mark.asyncio
async def test_async_stream_single_use_guard() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("data", 4)])),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    async for _ in stream:
        pass

    with pytest.raises(RuntimeError, match="single-use"):
        async for _ in stream:
            pass


# --- read() convenience tests ---


def test_sync_stream_read_text() -> None:
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert stream.read() == "hello world"


@pytest.mark.asyncio
async def test_async_stream_read_text() -> None:
    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)])),
        ]
    )

    stream = AsyncStreamReader(api, "/path")
    assert await stream.read() == "hello world"


# --- Transient HTTP error retry tests ---


@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
def test_sync_stream_retries_transient_http_error_on_reconnect(monkeypatch: pytest.MonkeyPatch, status: int) -> None:
    """A transient HTTP error during stream reconnect should be retried, not raised."""
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=httpx.ReadError("connection reset"))),
            _FakeSyncCM(_FakeSyncResponse([], status_code=status)),
            _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
        ]
    )

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    output = "".join(stream)

    assert output == "abcd"
    assert api.calls == [0, 2, 2]


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
async def test_async_stream_retries_transient_http_error_on_reconnect(
    monkeypatch: pytest.MonkeyPatch, status: int
) -> None:
    """A transient HTTP error during async stream reconnect should be retried, not raised."""

    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("ab", 2)], raise_after=httpx.ReadError("connection reset"))),
            _FakeAsyncCM(_FakeAsyncResponse([], status_code=status)),
            _FakeAsyncCM(_FakeAsyncResponse([_sse_event("cd", 4)])),
        ]
    )

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    chunks_list: list[str] = []
    async for chunk in stream:
        chunks_list.append(chunk)

    assert "".join(chunks_list) == "abcd"
    assert api.calls == [0, 2, 2]


@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
def test_sync_stream_transient_http_error_max_reconnects_exhausted(
    monkeypatch: pytest.MonkeyPatch, status: int
) -> None:
    """Repeated transient HTTP errors should eventually give up after MAX_RECONNECTS."""
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([], status_code=status)) for _ in range(MAX_RECONNECTS + 1)])

    stream = StreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(IsolaError):
        list(stream)


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
async def test_async_stream_transient_http_error_max_reconnects_exhausted(
    monkeypatch: pytest.MonkeyPatch, status: int
) -> None:
    """Repeated transient HTTP errors should eventually give up after MAX_RECONNECTS."""

    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([_FakeAsyncCM(_FakeAsyncResponse([], status_code=status)) for _ in range(MAX_RECONNECTS + 1)])

    stream = AsyncStreamReader(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(IsolaError):
        async for _ in stream:
            pass


# --- Stream natural termination tests ---


def test_sync_stream_read_completes_on_response_end() -> None:
    """read() completes when the HTTP response body ends naturally (no error)."""
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert stream.read() == "hello"


def test_sync_stream_iter_completes_on_response_end() -> None:
    """Iteration completes when the HTTP response body ends naturally."""
    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([_sse_event("hello", 5)])),
        ]
    )

    stream = StreamReader(api, "/path")
    assert list(stream) == ["hello"]
