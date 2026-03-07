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
import codecs
import contextlib
import time
from collections.abc import AsyncIterator, Generator, Iterator
from typing import Any, Protocol

import httpx

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
    """Single-use iterable stream with transparent reconnect.

    Parses SSE (Server-Sent Events) from the response. Data events carry
    stdout/stderr text, with `id:` fields tracking byte offsets for resume
    via the `Last-Event-ID` header. Comment lines (`: keepalive`) keep the
    connection alive through intermediate infrastructure.
    """

    def __init__(self, api: _SyncStreamAPI, path: str) -> None:
        self._api = api
        self._path = path
        self._offset = 0
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=None,
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

        while True:
            try:
                headers = {"Last-Event-ID": str(self._offset)} if self._offset > 0 else None
                with self._api.open_stream(
                    self._path,
                    headers=headers,
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, response.read())

                    data_parts: list[str] = []
                    event_id = ""
                    buf = ""

                    for chunk in response.iter_bytes():
                        if not chunk:
                            continue
                        buf += decoder.decode(chunk)
                        while "\n" in buf:
                            line, buf = buf.split("\n", 1)
                            line = line.rstrip("\r")
                            if not line:
                                # Empty line = event boundary
                                if event_id:
                                    with contextlib.suppress(ValueError):
                                        self._offset = int(event_id)
                                    event_id = ""
                                if data_parts:
                                    text = "\n".join(data_parts)
                                    data_parts = []
                                    reconnects = 0
                                    if text:
                                        yield text
                            elif line.startswith(":"):
                                pass  # comment (keepalive)
                            else:
                                name, _, value = line.partition(":")
                                if value.startswith(" "):
                                    value = value[1:]
                                if name == "data":
                                    data_parts.append(value)
                                elif name == "id" and "\0" not in value:
                                    event_id = value

                    final = decoder.decode(b"", final=True)
                    if final:
                        buf += final

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
    """Single-use async iterable stream with transparent reconnect.

    Parses SSE (Server-Sent Events) from the response. Data events carry
    stdout/stderr text, with `id:` fields tracking byte offsets for resume
    via the `Last-Event-ID` header. Comment lines (`: keepalive`) keep the
    connection alive through intermediate infrastructure.
    """

    def __init__(self, api: _AsyncStreamAPI, path: str) -> None:
        self._api = api
        self._path = path
        self._offset = 0
        self._httpx_timeout = httpx.Timeout(
            connect=STREAM_CONNECT_TIMEOUT,
            read=None,
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

        while True:
            try:
                headers = {"Last-Event-ID": str(self._offset)} if self._offset > 0 else None
                async with self._api.open_stream(
                    self._path,
                    headers=headers,
                    timeout=self._httpx_timeout,
                ) as response:
                    if response.status_code >= 400:
                        raise error_from_http(response.status_code, None, await response.aread())

                    data_parts: list[str] = []
                    event_id = ""
                    buf = ""

                    async for chunk in response.aiter_bytes():
                        if not chunk:
                            continue
                        buf += decoder.decode(chunk)
                        while "\n" in buf:
                            line, buf = buf.split("\n", 1)
                            line = line.rstrip("\r")
                            if not line:
                                # Empty line = event boundary
                                if event_id:
                                    with contextlib.suppress(ValueError):
                                        self._offset = int(event_id)
                                    event_id = ""
                                if data_parts:
                                    text = "\n".join(data_parts)
                                    data_parts = []
                                    reconnects = 0
                                    if text:
                                        yield text
                            elif line.startswith(":"):
                                pass  # comment (keepalive)
                            else:
                                name, _, value = line.partition(":")
                                if value.startswith(" "):
                                    value = value[1:]
                                if name == "data":
                                    data_parts.append(value)
                                elif name == "id" and "\0" not in value:
                                    event_id = value

                    final = decoder.decode(b"", final=True)
                    if final:
                        buf += final

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
