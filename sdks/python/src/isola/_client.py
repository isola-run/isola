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
import os
import time
from contextlib import AbstractAsyncContextManager, AbstractContextManager
from typing import Any, BinaryIO, TypeVar

import httpx
from pydantic import BaseModel, ValidationError

from ._exceptions import APIConnectionError, APIError, connection_error_from_request, error_from_http, is_transient

# https://www.python-httpx.org/advanced/timeouts/
# read / write timeouts are PER CHUNK, not total request timeouts
DEFAULT_TIMEOUT = httpx.Timeout(connect=5.0, read=30.0, write=30.0, pool=5.0)

MAX_RETRIES = 5
RETRY_DELAY = 1.0

ModelT = TypeVar("ModelT", bound=BaseModel)


def _stream_pos(content: bytes | BinaryIO | None) -> int | None:
    """Return the current position of a seekable stream, or None."""
    if hasattr(content, "seekable") and content.seekable():  # type: ignore[union-attr]
        return content.tell()  # type: ignore[union-attr]
    return None


class _SyncAPI:
    def __init__(self, url: str) -> None:
        self.url = _normalize_url(url)
        self._client = httpx.Client(timeout=DEFAULT_TIMEOUT)

    def close(self) -> None:
        self._client.close()

    def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> httpx.Response:
        url = f"{self.url}{path}"
        pos = _stream_pos(content)
        can_retry = pos is not None or content is None or isinstance(content, bytes)

        for attempt in range(1 + MAX_RETRIES):
            try:
                response = self._client.request(
                    method,
                    url,
                    params=params,
                    json=json_body,
                    content=content,
                    headers=headers,
                    timeout=timeout,
                )
            except httpx.RequestError as exc:
                if can_retry and attempt < MAX_RETRIES:
                    if pos is not None:
                        content.seek(pos)  # type: ignore[union-attr]
                    time.sleep(RETRY_DELAY)
                    continue
                raise connection_error_from_request(exc, method=method, path=path) from exc
            except Exception as exc:
                raise APIConnectionError(f"{method} {path}: {exc}") from exc

            if response.status_code >= 400:
                body = response.read()
                api_err = error_from_http(response.status_code, response.reason_phrase, body, method=method, path=path)
                if is_transient(api_err) and can_retry and attempt < MAX_RETRIES:
                    if pos is not None:
                        content.seek(pos)  # type: ignore[union-attr]
                    time.sleep(RETRY_DELAY)
                    continue
                raise api_err
            return response

        raise AssertionError("unreachable")

    def request_model(
        self,
        method: str,
        path: str,
        model: type[ModelT],
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
    ) -> ModelT:
        response = self.request(
            method,
            path,
            params=params,
            json_body=json_body,
            content=content,
            headers=headers,
        )
        try:
            payload = response.json()
            return model.model_validate(payload)
        except (ValueError, ValidationError) as exc:
            raise APIError(status_code=response.status_code, message="invalid response payload") from exc

    def request_bytes(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
    ) -> bytes:
        response = self.request(method, path, params=params)
        return response.content

    def request_no_content(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
    ) -> None:
        self.request(method, path, params=params, content=content, headers=headers)

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            f"{self.url}{path}",
            params=params,
            headers=headers,
            timeout=timeout,
        )


class _AsyncAPI:
    def __init__(self, url: str) -> None:
        self.url = _normalize_url(url)
        self._client = httpx.AsyncClient(timeout=DEFAULT_TIMEOUT)

    async def close(self) -> None:
        await self._client.aclose()

    async def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> httpx.Response:
        url = f"{self.url}{path}"
        pos = _stream_pos(content)
        can_retry = pos is not None or content is None or isinstance(content, bytes)

        for attempt in range(1 + MAX_RETRIES):
            try:
                response = await self._client.request(
                    method,
                    url,
                    params=params,
                    json=json_body,
                    content=content,
                    headers=headers,
                    timeout=timeout,
                )
            except httpx.RequestError as exc:
                if can_retry and attempt < MAX_RETRIES:
                    if pos is not None:
                        content.seek(pos)  # type: ignore[union-attr]
                    await asyncio.sleep(RETRY_DELAY)
                    continue
                raise connection_error_from_request(exc, method=method, path=path) from exc
            except Exception as exc:
                raise APIConnectionError(f"{method} {path}: {exc}") from exc

            if response.status_code >= 400:
                body = await response.aread()
                api_err = error_from_http(response.status_code, response.reason_phrase, body, method=method, path=path)
                if is_transient(api_err) and can_retry and attempt < MAX_RETRIES:
                    if pos is not None:
                        content.seek(pos)  # type: ignore[union-attr]
                    await asyncio.sleep(RETRY_DELAY)
                    continue
                raise api_err
            return response

        raise AssertionError("unreachable")

    async def request_model(
        self,
        method: str,
        path: str,
        model: type[ModelT],
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
    ) -> ModelT:
        response = await self.request(
            method,
            path,
            params=params,
            json_body=json_body,
            content=content,
            headers=headers,
        )
        try:
            payload = response.json()
            return model.model_validate(payload)
        except (ValueError, ValidationError) as exc:
            raise APIError(status_code=response.status_code, message="invalid response payload") from exc

    async def request_bytes(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
    ) -> bytes:
        response = await self.request(method, path, params=params)
        return response.content

    async def request_no_content(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        content: bytes | BinaryIO | None = None,
        headers: dict[str, str] | None = None,
    ) -> None:
        await self.request(method, path, params=params, content=content, headers=headers)

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractAsyncContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            f"{self.url}{path}",
            params=params,
            headers=headers,
            timeout=timeout,
        )


def _normalize_url(url: str) -> str:
    normalized = url.strip().rstrip("/")
    if not normalized:
        raise ValueError("url must not be empty")
    return normalized


class Isola:
    """Synchronous client for the Isola API.

    Example:

        from isola import Isola

        with Isola() as client:
            with client.sandboxes.create(image="alpine:3.21") as sandbox:
                result = sandbox.commands.run("echo", "hello")
                print(result.stdout)

    Args:
        url: Isola API URL. If not provided, reads from the
            ISOLA_URL environment variable.

    Raises:
        ValueError: If no URL is provided or found in environment.
    """

    def __init__(self, *, url: str | None = None) -> None:
        url = url or os.environ.get("ISOLA_URL")
        if not url:
            raise ValueError("url must be provided either as argument or via the ISOLA_URL environment variable")
        self._api = _SyncAPI(url)
        from ._rootfs_snapshot import RootfsSnapshots
        from ._sandbox import Sandboxes

        self.sandboxes = Sandboxes(self._api)
        self.rootfs_snapshots = RootfsSnapshots(self._api)

    def close(self) -> None:
        """Close the HTTP connection.

        Called automatically when using the client as a context manager.
        """
        self._api.close()

    def __enter__(self) -> Isola:
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()


class AsyncIsola:
    """Asynchronous client for the Isola API.

    Example:

        from isola import AsyncIsola

        async with AsyncIsola() as client:
            sandbox = await client.sandboxes.create(image="alpine:3.21")
            async with sandbox:
                result = await sandbox.commands.run("echo", "hello")
                print(result.stdout)

    Args:
        url: Isola API URL. If not provided, reads from the
            ISOLA_URL environment variable.

    Raises:
        ValueError: If no URL is provided or found in environment.
    """

    def __init__(self, *, url: str | None = None) -> None:
        url = url or os.environ.get("ISOLA_URL")
        if not url:
            raise ValueError("url must be provided either as argument or via the ISOLA_URL environment variable")
        self._api = _AsyncAPI(url)
        from ._rootfs_snapshot import AsyncRootfsSnapshots
        from ._sandbox import AsyncSandboxes

        self.sandboxes = AsyncSandboxes(self._api)
        self.rootfs_snapshots = AsyncRootfsSnapshots(self._api)

    async def close(self) -> None:
        """Close the HTTP connection.

        Called automatically when using the client as an async context
        manager.
        """
        await self._api.close()

    async def __aenter__(self) -> AsyncIsola:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.close()
