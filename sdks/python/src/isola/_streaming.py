from __future__ import annotations

import asyncio
import time
from collections.abc import AsyncIterator, Iterator
from contextlib import AbstractAsyncContextManager, AbstractContextManager
from types import TracebackType
from typing import Protocol

import httpx

from ._exceptions import APIConnectionError, StreamTimeoutError

STREAM_CONNECT_TIMEOUT = 5.0
MAX_RECONNECTS = 5
INITIAL_BACKOFF = 0.1
BACKOFF_FACTOR = 2.0
MAX_BACKOFF = 5.0


class _SyncStreamAPI(Protocol):
    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, int] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractContextManager[httpx.Response]: ...

    def raise_for_status(self, response: httpx.Response) -> None: ...

    def to_connection_error(self, exc: httpx.RequestError) -> APIConnectionError: ...


class _AsyncStreamAPI(Protocol):
    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, int] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractAsyncContextManager[httpx.Response]: ...

    async def raise_for_status(self, response: httpx.Response) -> None: ...

    def to_connection_error(self, exc: httpx.RequestError) -> APIConnectionError: ...


class CommandOutputStream:
    def __init__(
        self,
        api: _SyncStreamAPI,
        path: str,
        *,
        offset: int = 0,
        timeout: float | None = None,
    ) -> None:
        if offset < 0:
            raise ValueError("offset must be >= 0")
        if timeout is not None and timeout <= 0:
            raise ValueError("timeout must be > 0")

        self._api = api
        self._path = path
        self._offset = offset
        self._timeout = timeout
        self._stream_cm: AbstractContextManager[httpx.Response] | None = None

    def __enter__(self) -> Iterator[bytes]:
        return self._stream()

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self._close_stream(exc_type, exc, tb)

    def _make_timeout(self) -> httpx.Timeout:
        return httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=self._timeout,
            write=5.0,
            pool=5.0,
        )

    def _close_stream(
        self,
        exc_type: type[BaseException] | None = None,
        exc: BaseException | None = None,
        tb: TracebackType | None = None,
    ) -> None:
        if self._stream_cm is None:
            return

        stream_cm = self._stream_cm
        self._stream_cm = None
        stream_cm.__exit__(exc_type, exc, tb)

    def _stream(self) -> Iterator[bytes]:
        reconnects = 0
        backoff = INITIAL_BACKOFF

        while True:
            try:
                self._stream_cm = self._api.open_stream(
                    self._path,
                    params={"offset": self._offset},
                    timeout=self._make_timeout(),
                )
                response = self._stream_cm.__enter__()
                self._api.raise_for_status(response)

                for chunk in response.iter_bytes():
                    if not chunk:
                        continue
                    self._offset += len(chunk)
                    reconnects = 0
                    backoff = INITIAL_BACKOFF
                    yield chunk

                self._close_stream()
                return

            except httpx.ReadTimeout as exc:
                self._close_stream()
                raise StreamTimeoutError(f"No data received for {self._timeout}s") from exc
            except httpx.NetworkError as exc:
                self._close_stream()
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise self._api.to_connection_error(exc) from exc
                time.sleep(min(backoff, MAX_BACKOFF))
                backoff *= BACKOFF_FACTOR


class AsyncCommandOutputStream:
    def __init__(
        self,
        api: _AsyncStreamAPI,
        path: str,
        *,
        offset: int = 0,
        timeout: float | None = None,
    ) -> None:
        if offset < 0:
            raise ValueError("offset must be >= 0")
        if timeout is not None and timeout <= 0:
            raise ValueError("timeout must be > 0")

        self._api = api
        self._path = path
        self._offset = offset
        self._timeout = timeout
        self._stream_cm: AbstractAsyncContextManager[httpx.Response] | None = None

    async def __aenter__(self) -> AsyncIterator[bytes]:
        return self._stream()

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self._close_stream(exc_type, exc, tb)

    def _make_timeout(self) -> httpx.Timeout:
        return httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=self._timeout,
            write=5.0,
            pool=5.0,
        )

    async def _close_stream(
        self,
        exc_type: type[BaseException] | None = None,
        exc: BaseException | None = None,
        tb: TracebackType | None = None,
    ) -> None:
        if self._stream_cm is None:
            return

        stream_cm = self._stream_cm
        self._stream_cm = None
        await stream_cm.__aexit__(exc_type, exc, tb)

    async def _stream(self) -> AsyncIterator[bytes]:
        reconnects = 0
        backoff = INITIAL_BACKOFF

        while True:
            try:
                self._stream_cm = self._api.open_stream(
                    self._path,
                    params={"offset": self._offset},
                    timeout=self._make_timeout(),
                )
                response = await self._stream_cm.__aenter__()
                await self._api.raise_for_status(response)

                async for chunk in response.aiter_bytes():
                    if not chunk:
                        continue
                    self._offset += len(chunk)
                    reconnects = 0
                    backoff = INITIAL_BACKOFF
                    yield chunk

                await self._close_stream()
                return

            except httpx.ReadTimeout as exc:
                await self._close_stream()
                raise StreamTimeoutError(f"No data received for {self._timeout}s") from exc
            except httpx.NetworkError as exc:
                await self._close_stream()
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise self._api.to_connection_error(exc) from exc
                await asyncio.sleep(min(backoff, MAX_BACKOFF))
                backoff *= BACKOFF_FACTOR
