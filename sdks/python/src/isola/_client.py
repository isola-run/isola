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


class _SyncAPI:
    def __init__(self, base_url: str) -> None:
        self.base_url = _normalize_base_url(base_url)
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
        url = f"{self.base_url}{path}"

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
                if attempt < MAX_RETRIES:
                    time.sleep(RETRY_DELAY)
                    continue
                raise connection_error_from_request(exc, method=method, path=path) from exc
            except Exception as exc:
                raise APIConnectionError(f"{method} {path}: {exc}") from exc

            if response.status_code >= 400:
                body = response.read()
                api_err = error_from_http(response.status_code, response.reason_phrase, body, method=method, path=path)
                if is_transient(api_err) and attempt < MAX_RETRIES:
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
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            f"{self.base_url}{path}",
            params=params,
            timeout=timeout,
        )


class _AsyncAPI:
    def __init__(self, base_url: str) -> None:
        self.base_url = _normalize_base_url(base_url)
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
        url = f"{self.base_url}{path}"

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
                if attempt < MAX_RETRIES:
                    await asyncio.sleep(RETRY_DELAY)
                    continue
                raise connection_error_from_request(exc, method=method, path=path) from exc
            except Exception as exc:
                raise APIConnectionError(f"{method} {path}: {exc}") from exc

            if response.status_code >= 400:
                body = await response.aread()
                api_err = error_from_http(response.status_code, response.reason_phrase, body, method=method, path=path)
                if is_transient(api_err) and attempt < MAX_RETRIES:
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
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractAsyncContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            f"{self.base_url}{path}",
            params=params,
            timeout=timeout,
        )


def _normalize_base_url(base_url: str) -> str:
    normalized = base_url.strip().rstrip("/")
    if not normalized:
        raise ValueError("base_url must not be empty")
    return normalized


class Isola:
    def __init__(self, *, base_url: str | None = None) -> None:
        base_url = base_url or os.environ.get("ISOLA_BASE_URL")
        if not base_url:
            raise ValueError(
                "base_url must be provided either as argument or via the ISOLA_BASE_URL environment variable"
            )
        self._api = _SyncAPI(base_url)
        from ._sandbox import Sandboxes

        self.sandboxes = Sandboxes(self._api)

    def close(self) -> None:
        self._api.close()

    def __enter__(self) -> Isola:
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()


class AsyncIsola:
    def __init__(self, *, base_url: str | None = None) -> None:
        base_url = base_url or os.environ.get("ISOLA_BASE_URL")
        if not base_url:
            raise ValueError(
                "base_url must be provided either as argument or via the ISOLA_BASE_URL environment variable"
            )
        self._api = _AsyncAPI(base_url)
        from ._sandbox import AsyncSandboxes

        self.sandboxes = AsyncSandboxes(self._api)

    async def close(self) -> None:
        await self._api.close()

    async def __aenter__(self) -> AsyncIsola:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.close()
