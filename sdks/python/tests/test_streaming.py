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

_SSE_HEADERS: dict[str, str] = {"content-type": "text/event-stream"}


def _sse_event(data: str, event_id: int) -> str:
    """Format a single SSE data event as wire text."""
    lines = [f"data: {line}" for line in data.split("\n")]
    lines.append(f"id: {event_id}")
    lines.append("")  # blank line terminates event
    return "\n".join(lines) + "\n"


# ---------------------------------------------------------------------------
# Fakes
# ---------------------------------------------------------------------------


class _FakeSyncResponse:
    def __init__(self, chunks: list[str], *, raise_after: Exception | None = None, status_code: int = 200) -> None:
        self.status_code = status_code
        self.headers = _SSE_HEADERS
        self._chunks = chunks
        self._raise_after = raise_after

    def iter_text(self) -> Iterator[str]:
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
    def __init__(self, chunks: list[str], *, raise_after: Exception | None = None, status_code: int = 200) -> None:
        self.status_code = status_code
        self.headers = _SSE_HEADERS
        self._chunks = chunks
        self._raise_after = raise_after

    async def aiter_text(self) -> AsyncIterator[str]:
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


# ---------------------------------------------------------------------------
# Sync: core behavior
# ---------------------------------------------------------------------------


def test_sync_stream_yields_data() -> None:
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)]))])

    assert list(StreamReader(api, "/path")) == ["hello ", "world"]


def test_sync_stream_read() -> None:
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)]))])

    assert StreamReader(api, "/path").read() == "hello world"


def test_sync_stream_single_use() -> None:
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([_sse_event("x", 1)]))])

    stream = StreamReader(api, "/path")
    list(stream)

    with pytest.raises(RuntimeError, match="single-use"):
        list(stream)


# ---------------------------------------------------------------------------
# Sync: SSE integration
# ---------------------------------------------------------------------------


def test_sync_stream_multiline_data() -> None:
    """Multiple data: lines in one event are joined with newlines."""
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse(["data: hello\ndata: world\nid: 11\n\n"]))])

    assert StreamReader(api, "/path").read() == "hello\nworld"


def test_sync_stream_filters_keepalive() -> None:
    """SSE comment lines are invisible to the consumer."""
    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([
            _sse_event("hello", 5),
            ": keepalive\n\n",
            _sse_event("world", 10),
        ]))
    ])

    assert StreamReader(api, "/path").read() == "helloworld"


def test_sync_stream_preserves_trailing_newline() -> None:
    """Output ending with \\n round-trips through SSE (extra empty data: line)."""
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse(["data: hello\ndata: \nid: 6\n\n"]))])

    assert StreamReader(api, "/path").read() == "hello\n"


# ---------------------------------------------------------------------------
# Sync: reconnection
# ---------------------------------------------------------------------------


def test_sync_stream_reconnects_and_resumes(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=httpx.ConnectError("drop"))),
        _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
    ])

    assert StreamReader(api, "/path").read() == "abcd"
    assert api.calls == [0, 2]


@pytest.mark.parametrize(
    "error",
    [
        httpx.ConnectError("refused"),
        httpx.ReadError("reset"),
        httpx.WriteError("broken pipe"),
        httpx.ConnectTimeout("timeout"),
    ],
    ids=["ConnectError", "ReadError", "WriteError", "ConnectTimeout"],
)
def test_sync_stream_reconnects_on_network_error(monkeypatch: pytest.MonkeyPatch, error: Exception) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=error)),
        _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
    ])

    assert StreamReader(api, "/path").read() == "abcd"
    assert api.calls == [0, 2]


def test_sync_stream_reconnects_on_enter_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(enter_exc=httpx.ConnectError("refused")),
        _FakeSyncCM(_FakeSyncResponse([_sse_event("hello", 5)])),
    ])

    assert StreamReader(api, "/path").read() == "hello"
    assert api.calls == [0, 0]


def test_sync_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([_FakeSyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)])

    with pytest.raises(APIConnectionError):
        list(StreamReader(api, "/path"))


# ---------------------------------------------------------------------------
# Sync: HTTP error handling
# ---------------------------------------------------------------------------


def test_sync_stream_http_error_propagates() -> None:
    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([], status_code=404))])

    with pytest.raises(NotFoundError):
        list(StreamReader(api, "/path"))


@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
def test_sync_stream_retries_transient_http_error(monkeypatch: pytest.MonkeyPatch, status: int) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([_sse_event("ab", 2)], raise_after=httpx.ReadError("reset"))),
        _FakeSyncCM(_FakeSyncResponse([], status_code=status)),
        _FakeSyncCM(_FakeSyncResponse([_sse_event("cd", 4)])),
    ])

    assert StreamReader(api, "/path").read() == "abcd"
    assert api.calls == [0, 2, 2]


@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
def test_sync_stream_transient_http_max_reconnects(monkeypatch: pytest.MonkeyPatch, status: int) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([_FakeSyncCM(_FakeSyncResponse([], status_code=status)) for _ in range(MAX_RECONNECTS + 1)])

    with pytest.raises(IsolaError):
        list(StreamReader(api, "/path"))


# ---------------------------------------------------------------------------
# Async: core behavior
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_async_stream_yields_data() -> None:
    api = _FakeAsyncAPI([_FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)]))])

    assert [c async for c in AsyncStreamReader(api, "/path")] == ["hello ", "world"]


@pytest.mark.asyncio
async def test_async_stream_read() -> None:
    api = _FakeAsyncAPI([_FakeAsyncCM(_FakeAsyncResponse([_sse_event("hello ", 6), _sse_event("world", 11)]))])

    assert await AsyncStreamReader(api, "/path").read() == "hello world"


@pytest.mark.asyncio
async def test_async_stream_single_use() -> None:
    api = _FakeAsyncAPI([_FakeAsyncCM(_FakeAsyncResponse([_sse_event("x", 1)]))])

    stream = AsyncStreamReader(api, "/path")
    async for _ in stream:
        pass

    with pytest.raises(RuntimeError, match="single-use"):
        async for _ in stream:
            pass


# ---------------------------------------------------------------------------
# Async: reconnection
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_async_stream_reconnects_and_resumes(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        pass

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([_sse_event("a", 1)], raise_after=httpx.ConnectError("drop"))),
        _FakeAsyncCM(_FakeAsyncResponse([_sse_event("b", 2), _sse_event("c", 3)])),
    ])

    assert await AsyncStreamReader(api, "/path").read() == "abc"
    assert api.calls == [0, 1]


@pytest.mark.asyncio
async def test_async_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        pass

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([_FakeAsyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)])

    with pytest.raises(APIConnectionError):
        async for _ in AsyncStreamReader(api, "/path"):
            pass


# ---------------------------------------------------------------------------
# Async: HTTP error handling
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_async_stream_http_error_propagates() -> None:
    api = _FakeAsyncAPI([_FakeAsyncCM(_FakeAsyncResponse([], status_code=404))])

    with pytest.raises(NotFoundError):
        async for _ in AsyncStreamReader(api, "/path"):
            pass


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [502, 503, 504], ids=["502", "503", "504"])
async def test_async_stream_retries_transient_http_error(monkeypatch: pytest.MonkeyPatch, status: int) -> None:
    async def _no_sleep(_: float) -> None:
        pass

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([_sse_event("ab", 2)], raise_after=httpx.ReadError("reset"))),
        _FakeAsyncCM(_FakeAsyncResponse([], status_code=status)),
        _FakeAsyncCM(_FakeAsyncResponse([_sse_event("cd", 4)])),
    ])

    assert await AsyncStreamReader(api, "/path").read() == "abcd"
    assert api.calls == [0, 2, 2]
