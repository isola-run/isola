from __future__ import annotations

import asyncio
import time
from collections.abc import AsyncGenerator, AsyncIterator, Generator, Iterator
from typing import Any, Protocol

import httpx

from ._exceptions import StreamTimeoutError, connection_error_from_request, error_from_http

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
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> Any: ...


class _AsyncStreamAPI(Protocol):
    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> Any: ...


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
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=timeout,
            write=5.0,
            pool=5.0,
        )
        self._gen: Generator[bytes, None, None] | None = None

    def __enter__(self) -> Iterator[bytes]:
        self._gen = self._stream()
        return self._gen

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: type[BaseException] | None,
    ) -> None:
        if self._gen is not None:
            self._gen.close()
            self._gen = None

    def _stream(self) -> Generator[bytes, None, None]:
        reconnects = 0
        backoff = INITIAL_BACKOFF

        while True:
            try:
                with self._api.open_stream(
                    self._path,
                    params={"offset": self._offset},
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, response.read())

                    for chunk in response.iter_bytes():
                        if not chunk:
                            continue
                        self._offset += len(chunk)
                        reconnects = 0
                        backoff = INITIAL_BACKOFF
                        yield chunk

                    return

            except httpx.ReadTimeout as exc:
                raise StreamTimeoutError(f"No data received for {self._httpx_timeout.read}s") from exc
            except httpx.NetworkError as exc:
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise connection_error_from_request(exc) from exc
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
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=timeout,
            write=5.0,
            pool=5.0,
        )
        self._gen: AsyncGenerator[bytes, None] | None = None

    async def __aenter__(self) -> AsyncIterator[bytes]:
        self._gen = self._stream()
        return self._gen

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: type[BaseException] | None,
    ) -> None:
        if self._gen is not None:
            await self._gen.aclose()
            self._gen = None

    async def _stream(self) -> AsyncGenerator[bytes, None]:
        reconnects = 0
        backoff = INITIAL_BACKOFF

        while True:
            try:
                async with self._api.open_stream(
                    self._path,
                    params={"offset": self._offset},
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, await response.aread())

                    async for chunk in response.aiter_bytes():
                        if not chunk:
                            continue
                        self._offset += len(chunk)
                        reconnects = 0
                        backoff = INITIAL_BACKOFF
                        yield chunk

                    return

            except httpx.ReadTimeout as exc:
                raise StreamTimeoutError(f"No data received for {self._httpx_timeout.read}s") from exc
            except httpx.NetworkError as exc:
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise connection_error_from_request(exc) from exc
                await asyncio.sleep(min(backoff, MAX_BACKOFF))
                backoff *= BACKOFF_FACTOR
