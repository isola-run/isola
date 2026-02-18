from __future__ import annotations

from collections.abc import AsyncIterator, Iterator

import httpx
import pytest

from isola import ConnectionError as IsolaConnectionError
from isola import NotFoundError, StreamTimeoutError
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

    def raise_for_status(self, response: _FakeSyncResponse) -> None:
        if response.status_code >= 400:
            from isola._exceptions import error_from_http

            raise error_from_http(response.status_code, None, response.read())

    def to_connection_error(self, exc: httpx.RequestError) -> IsolaConnectionError:
        return IsolaConnectionError(str(exc))


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

    def read(self) -> bytes:
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

    async def raise_for_status(self, response: _FakeAsyncResponse) -> None:
        if response.status_code >= 400:
            from isola._exceptions import error_from_http

            raise error_from_http(response.status_code, None, response.read())

    def to_connection_error(self, exc: httpx.RequestError) -> IsolaConnectionError:
        return IsolaConnectionError(str(exc))


def test_sync_stream_reconnects_and_resumes_offset(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(_FakeSyncResponse([b"ab"], raise_after=httpx.ConnectError("disconnect"))),
            _FakeSyncCM(_FakeSyncResponse([b"cd"])),
        ]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

    with stream as chunks:
        output = b"".join(chunks)

    assert output == b"abcd"
    assert api.calls == [0, 2]


def test_sync_stream_timeout_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([], raise_after=httpx.ReadTimeout("idle"))),
    ])

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", timeout=60)

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

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

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

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout", timeout=10)

    with pytest.raises(StreamTimeoutError):
        async with stream as chunks:
            async for _ in chunks:
                pass


def test_sync_stream_max_reconnects_exhausted(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [_FakeSyncCM(enter_exc=httpx.ConnectError("down")) for _ in range(MAX_RECONNECTS + 1)]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(IsolaConnectionError), stream as chunks:
        list(chunks)


def test_sync_stream_connect_error_during_enter(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI(
        [
            _FakeSyncCM(enter_exc=httpx.ConnectError("refused")),
            _FakeSyncCM(_FakeSyncResponse([b"hello"])),
        ]
    )

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

    with stream as chunks:
        output = b"".join(chunks)

    assert output == b"hello"
    assert api.calls == [0, 0]


def test_sync_stream_http_error_propagates(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._streaming.time.sleep", lambda _: None)

    api = _FakeSyncAPI([
        _FakeSyncCM(_FakeSyncResponse([], status_code=404)),
    ])

    stream = CommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

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

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(IsolaConnectionError):
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

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

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

    stream = AsyncCommandOutputStream(api, "/sandboxes/s-1/commands/c-1/stdout")

    with pytest.raises(NotFoundError):
        async with stream as chunks:
            async for _ in chunks:
                pass
