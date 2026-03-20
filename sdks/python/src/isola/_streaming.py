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

import asyncio
import contextlib
import time
from collections.abc import AsyncIterator, Generator, Iterator
from typing import Any, Protocol

import httpx
from httpx_sse import EventSource

from ._exceptions import APIError, connection_error_from_request, error_from_http, is_transient

STREAM_CONNECT_TIMEOUT = 5.0
STREAM_WRITE_TIMEOUT = 15.0
STREAM_POOL_TIMEOUT = 5.0
MAX_RECONNECTS = 5
RETRY_DELAY = 1.0


class _SyncStreamAPI(Protocol):
    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> Any: ...


class _AsyncStreamAPI(Protocol):
    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> Any: ...


class StreamReader:
    """Single-use iterable stream with transparent reconnect."""

    def __init__(self, api: _SyncStreamAPI, path: str) -> None:
        self._api = api
        self._path = path
        self._last_event_id: int | None = None
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=None,  # wait forever / until server error for data
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
        reconnects = 0

        while True:
            try:
                headers = {"Last-Event-ID": str(self._last_event_id)} if self._last_event_id is not None else None
                with self._api.open_stream(
                    self._path,
                    headers=headers,
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, response.read())

                    for sse in EventSource(response).iter_sse():
                        if sse.id:
                            with contextlib.suppress(ValueError):
                                self._last_event_id = int(sse.id)
                        if sse.data:
                            reconnects = 0
                            yield sse.data

                    return

            except (httpx.RequestError, APIError) as exc:
                if isinstance(exc, APIError) and not is_transient(exc):
                    raise
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    if isinstance(exc, httpx.RequestError):
                        raise connection_error_from_request(exc) from exc
                    raise
                time.sleep(RETRY_DELAY)


class AsyncStreamReader:
    """Single-use async iterable stream with transparent reconnect."""

    def __init__(self, api: _AsyncStreamAPI, path: str) -> None:
        self._api = api
        self._path = path
        self._last_event_id: int | None = None
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=None,  # wait forever / until server error for data
            write=STREAM_WRITE_TIMEOUT,
            pool=STREAM_POOL_TIMEOUT,
        )
        self._consumed = False

    async def __aiter__(self) -> AsyncIterator[str]:
        if self._consumed:
            raise RuntimeError("AsyncStreamReader is single-use and has already been consumed")
        self._consumed = True

        reconnects = 0

        while True:
            try:
                headers = {"Last-Event-ID": str(self._last_event_id)} if self._last_event_id is not None else None
                async with self._api.open_stream(
                    self._path,
                    headers=headers,
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, await response.aread())

                    async for sse in EventSource(response).aiter_sse():
                        if sse.id:
                            with contextlib.suppress(ValueError):
                                self._last_event_id = int(sse.id)
                        if sse.data:
                            reconnects = 0
                            yield sse.data

                    return

            except (httpx.RequestError, APIError) as exc:
                if isinstance(exc, APIError) and not is_transient(exc):
                    raise
                reconnects += 1
                if reconnects > MAX_RECONNECTS:
                    if isinstance(exc, httpx.RequestError):
                        raise connection_error_from_request(exc) from exc
                    raise
                await asyncio.sleep(RETRY_DELAY)

    async def read(self) -> str:
        return "".join([chunk async for chunk in self])
