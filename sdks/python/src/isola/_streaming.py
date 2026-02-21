from __future__ import annotations

import asyncio
import codecs
import time
from collections.abc import AsyncIterator, Generator, Iterator
from typing import Any, Protocol

import httpx

from ._exceptions import StreamTimeoutError, connection_error_from_request, error_from_http

STREAM_CONNECT_TIMEOUT = 5.0
STREAM_WRITE_TIMEOUT = 5.0
STREAM_POOL_TIMEOUT = 5.0
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


class StreamReader:
    """Single-use iterable stream with transparent reconnect."""

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
            write=STREAM_WRITE_TIMEOUT,
            pool=STREAM_POOL_TIMEOUT,
        )
        self._consumed = False

    def __iter__(self) -> Iterator[str]:
        if self._consumed:
            raise RuntimeError("StreamReader is single-use and has already been consumed")
        self._consumed = True
        return self._generate()

    def read(self) -> str:
        return "".join(self)

    def _generate(self) -> Generator[str, None, None]:
        decoder = codecs.getincrementaldecoder("utf-8")("replace")
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
                        decoded = decoder.decode(chunk)
                        if decoded:
                            yield decoded

                    final = decoder.decode(b"", final=True)
                    if final:
                        yield final

                    return

            except httpx.ReadTimeout as exc:
                raise StreamTimeoutError(f"No data received for {self._httpx_timeout.read}s") from exc
            except (httpx.NetworkError, httpx.ConnectTimeout) as exc:
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise connection_error_from_request(exc) from exc
                time.sleep(backoff)
                backoff = min(backoff * BACKOFF_FACTOR, MAX_BACKOFF)


class AsyncStreamReader:
    """Single-use async iterable stream with transparent reconnect."""

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
            write=STREAM_WRITE_TIMEOUT,
            pool=STREAM_POOL_TIMEOUT,
        )
        self._consumed = False

    async def __aiter__(self) -> AsyncIterator[str]:
        if self._consumed:
            raise RuntimeError("AsyncStreamReader is single-use and has already been consumed")
        self._consumed = True

        decoder = codecs.getincrementaldecoder("utf-8")("replace")
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
                        decoded = decoder.decode(chunk)
                        if decoded:
                            yield decoded

                    final = decoder.decode(b"", final=True)
                    if final:
                        yield final

                    return

            except httpx.ReadTimeout as exc:
                raise StreamTimeoutError(f"No data received for {self._httpx_timeout.read}s") from exc
            except (httpx.NetworkError, httpx.ConnectTimeout) as exc:
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    raise connection_error_from_request(exc) from exc
                await asyncio.sleep(backoff)
                backoff = min(backoff * BACKOFF_FACTOR, MAX_BACKOFF)

    async def read(self) -> str:
        return "".join([chunk async for chunk in self])
