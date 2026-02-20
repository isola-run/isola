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

from isola import APIConnectionError, NotFoundError, StreamTimeoutError
from isola._streaming import MAX_RECONNECTS, AsyncCommandOutputStream, CommandOutputStream


class _FakeSyncResponse:
    def __init__(
        self, chunks: list[bytes], *, raise_after: Exception | None = None, status_code: int = 200
    ) -> None:
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

    def open_stream(self, path: str, *, params: dict[str, int] | None = None, timeout: object = None) -> _FakeSyncCM:
        del path, timeout
        assert params is not None
        self.calls.append(params["offset"])
        return self._sequence.pop(0)


class _FakeAsyncResponse:
    def __init__(
        self, chunks: list[bytes], *, raise_after: Exception | None = None, status_code: int = 200
    ) -> None:
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

    def open_stream(self, path: str, *, params: dict[str, int] | None = None, timeout: object = None) -> _FakeAsyncCM:
        del path, timeout
        assert params is not None
        self.calls.append(params["offset"])
        return self._sequence.pop(0)


def test_sync_stream_reconnects_and_resumes_offset(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"ab"], raise_after=httpx.ConnectError("disconnect"))),
            _FakeSyncCM(_FakeSyncResponse([b"cd"])),
        ]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with stream as chunks:
        output = b"".join(chunks)

    assert output == b"abcd"
    assert api.calls == [0, 2]


@pytest.mark.parametrize(
    "error",
    [
        httpx.ConnectError("connection refused"),
        httpx.ReadError("server dropped"),
        httpx.WriteError("broken pipe"),
        httpx.CloseError("close failed"),
    ],
    ids=["ConnectError", "ReadError", "WriteError", "CloseError"],
)
def test_sync_stream_reconnects_on_network_error(monkeypatch: pytest.MonkeyPatch, error: Exception) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"ab"], raise_after=error)),
            _FakeSyncCM(_FakeSyncResponse([b"cd"])),
        ]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with stream as chunks:
        output = b"".join(chunks)

    assert output == b"abcd"
    assert api.calls == [0, 2]


def test_sync_stream_timeout_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([], raise_after=httpx.ReadTimeout("idle"))),
    ])

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", timeout=60, text=False)

    with pytest.raises(StreamTimeoutError), stream as chunks:
        list(chunks)


@pytest.mark.asyncio
async def test_async_stream_reconnects_and_resumes_offset(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(_FakeAsyncResponse([b"a"], raise_after=httpx.ConnectError("disconnect"))),
            _FakeAsyncCM(_FakeAsyncResponse([b"b", b"c"])),
        ]
    )

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    chunks_list: list[bytes] = []
    async with stream as chunks:
        async for chunk in chunks:
            chunks_list.append(chunk)

    assert b"".join(chunks_list) == b"abc"
    assert api.calls == [0, 1]


@pytest.mark.asyncio
async def test_async_stream_timeout_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([], raise_after=httpx.ReadTimeout("idle"))),
    ])

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", timeout=10, text=False)

    with pytest.raises(StreamTimeoutError):
        async with stream as chunks:
            async for _ in chunks:
                pass


def test_sync_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [_FakeSyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with pytest.raises(APIConnectionError), stream as chunks:
        list(chunks)


def test_sync_stream_connect_error_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(enter_exc=httpx.ConnectError("refused")),
            _FakeSyncCM(_FakeSyncResponse([b"hello"])),
        ]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with stream as chunks:
        output = b"".join(chunks)

    assert output == b"hello"
    assert api.calls == [0, 0]


def test_sync_stream_http_error_propagates(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([], status_code=404)),
    ])

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with pytest.raises(NotFoundError), stream as chunks:
        list(chunks)


@pytest.mark.asyncio
async def test_async_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [_FakeAsyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)]
    )

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with pytest.raises(APIConnectionError):
        async with stream as chunks:
            async for _ in chunks:
                pass


@pytest.mark.asyncio
async def test_async_stream_connect_error_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI(
        [
            _FakeAsyncCM(enter_exc=httpx.ConnectError("refused")),
            _FakeAsyncCM(_FakeAsyncResponse([b"hello"])),
        ]
    )

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    chunks_list: list[bytes] = []
    async with stream as chunks:
        async for chunk in chunks:
            chunks_list.append(chunk)

    assert b"".join(chunks_list) == b"hello"
    assert api.calls == [0, 0]


@pytest.mark.asyncio
async def test_async_stream_http_error_propagates(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_seconds: float) -> None:
        return None

    monkeypatch.setattr("isola._streaming.asyncio.sleep", _no_sleep)

    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([], status_code=404)),
    ])

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", text=False)

    with pytest.raises(NotFoundError):
        async with stream as chunks:
            async for _ in chunks:
                pass


@pytest.mark.parametrize(
    ("offset", "timeout", "match"),
    [
        (-1, None, "offset must be >= 0"),
        (0, 0, "timeout must be > 0"),
        (0, -5, "timeout must be > 0"),
    ],
    ids=["negative_offset", "zero_timeout", "negative_timeout"],
)
def test_sync_stream_rejects_invalid_params(offset: int, timeout: float | None, match: str) -> None:
    with pytest.raises(ValueError, match=match):
        CommandOutputStream(object(), "/path", offset=offset, timeout=timeout)


@pytest.mark.parametrize(
    ("offset", "timeout", "match"),
    [
        (-1, None, "offset must be >= 0"),
        (0, 0, "timeout must be > 0"),
        (0, -5, "timeout must be > 0"),
    ],
    ids=["negative_offset", "zero_timeout", "negative_timeout"],
)
def test_async_stream_rejects_invalid_params(offset: int, timeout: float | None, match: str) -> None:
    with pytest.raises(ValueError, match=match):
        AsyncCommandOutputStream(object(), "/path", offset=offset, timeout=timeout)


# --- Text mode tests ---


def test_sync_stream_text_mode_decodes_utf8() -> None:
    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([b"hello ", b"world"])),
    ])

    stream = CommandOutputStream(api, "/path", text=True)

    with stream as chunks:
        output = "".join(chunks)

    assert output == "hello world"


def test_sync_stream_text_mode_handles_split_multibyte() -> None:
    # "café\n" in UTF-8: b"caf\xc3\xa9\n"
    # Split the é (0xc3 0xa9) across two chunks
    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([b"caf\xc3", b"\xa9\n"])),
    ])

    stream = CommandOutputStream(api, "/path", text=True)

    with stream as chunks:
        output = "".join(chunks)

    assert output == "caf\u00e9\n"


def test_sync_stream_binary_mode_yields_bytes() -> None:
    data = b"\x00\x01\x02\xff"
    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([data])),
    ])

    stream = CommandOutputStream(api, "/path", text=False)

    with stream as chunks:
        output = b"".join(chunks)

    assert output == data


@pytest.mark.asyncio
async def test_async_stream_text_mode_decodes_utf8() -> None:
    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([b"hello ", b"world"])),
    ])

    stream = AsyncCommandOutputStream(api, "/path", text=True)

    chunks_list: list[str] = []
    async with stream as chunks:
        async for chunk in chunks:
            chunks_list.append(chunk)

    assert "".join(chunks_list) == "hello world"


@pytest.mark.asyncio
async def test_async_stream_text_mode_handles_split_multibyte() -> None:
    # "café\n" in UTF-8: b"caf\xc3\xa9\n"
    # Split the é (0xc3 0xa9) across two chunks
    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([b"caf\xc3", b"\xa9\n"])),
    ])

    stream = AsyncCommandOutputStream(api, "/path", text=True)

    chunks_list: list[str] = []
    async with stream as chunks:
        async for chunk in chunks:
            chunks_list.append(chunk)

    assert "".join(chunks_list) == "caf\u00e9\n"


def test_sync_stream_text_mode_flushes_incomplete_sequence_at_eof() -> None:
    # Stream ends with 0xc3 — the first byte of a 2-byte UTF-8 character
    # with no second byte. The decoder should flush it as U+FFFD (replacement).
    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([b"hello\xc3"])),
    ])

    stream = CommandOutputStream(api, "/path", text=True)

    with stream as chunks:
        output = "".join(chunks)

    assert output == "hello\ufffd"


@pytest.mark.asyncio
async def test_async_stream_text_mode_flushes_incomplete_sequence_at_eof() -> None:
    api = _FakeAsyncAPI([
        _FakeAsyncCM(_FakeAsyncResponse([b"hello\xc3"])),
    ])

    stream = AsyncCommandOutputStream(api, "/path", text=True)

    chunks_list: list[str] = []
    async with stream as chunks:
        async for chunk in chunks:
            chunks_list.append(chunk)

    assert "".join(chunks_list) == "hello\ufffd"
