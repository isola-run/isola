from __future__ import annotations

from contextlib import AbstractAsyncContextManager, AbstractContextManager
from typing import Any, TypeVar

import httpx
from pydantic import BaseModel, ValidationError

from ._exceptions import APIConnectionError, IsolaError, connection_error_from_request, error_from_http

DEFAULT_TIMEOUT = httpx.Timeout(10.0, connect=5.0)

ModelT = TypeVar("ModelT", bound=BaseModel)


class _SyncAPI:
    def __init__(self, base_url: str, client: httpx.Client | None = None) -> None:
        self.base_url = _normalize_base_url(base_url)
        if client is None:
            self._client = httpx.Client(timeout=DEFAULT_TIMEOUT)
            self._owns_client = True
        else:
            self._client = client
            self._owns_client = False

    def close(self) -> None:
        if self._owns_client:
            self._client.close()

    def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> httpx.Response:
        try:
            response = self._client.request(
                method,
                self.url_for(path),
                params=params,
                json=json_body,
                content=content,
                headers=headers,
                timeout=timeout,
            )
        except httpx.RequestError as exc:
            raise connection_error_from_request(exc) from exc

        self.raise_for_status(response)
        return response

    def request_model(
        self,
        method: str,
        path: str,
        model: type[ModelT],
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> ModelT:
        response = self.request(
            method,
            path,
            params=params,
            json_body=json_body,
            content=content,
            headers=headers,
            timeout=timeout,
        )
        try:
            payload = response.json()
            return model.model_validate(payload)
        except (ValueError, ValidationError) as exc:
            raise IsolaError(status=response.status_code, detail="invalid response payload") from exc

    def request_bytes(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> bytes:
        response = self.request(method, path, params=params, timeout=timeout)
        return response.content

    def request_no_content(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> None:
        self.request(method, path, params=params, content=content, headers=headers, timeout=timeout)

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            self.url_for(path),
            params=params,
            timeout=timeout,
        )

    def raise_for_status(self, response: httpx.Response) -> None:
        if response.status_code < 400:
            return
        body = response.read()
        raise error_from_http(response.status_code, response.reason_phrase, body)

    def to_connection_error(self, exc: httpx.RequestError) -> APIConnectionError:
        return connection_error_from_request(exc)

    def url_for(self, path: str) -> str:
        if path.startswith("/"):
            return f"{self.base_url}{path}"
        return f"{self.base_url}/{path}"


class _AsyncAPI:
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self.base_url = _normalize_base_url(base_url)
        if client is None:
            self._client = httpx.AsyncClient(timeout=DEFAULT_TIMEOUT)
            self._owns_client = True
        else:
            self._client = client
            self._owns_client = False

    async def close(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def request(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> httpx.Response:
        try:
            response = await self._client.request(
                method,
                self.url_for(path),
                params=params,
                json=json_body,
                content=content,
                headers=headers,
                timeout=timeout,
            )
        except httpx.RequestError as exc:
            raise connection_error_from_request(exc) from exc

        await self.raise_for_status(response)
        return response

    async def request_model(
        self,
        method: str,
        path: str,
        model: type[ModelT],
        *,
        params: dict[str, Any] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> ModelT:
        response = await self.request(
            method,
            path,
            params=params,
            json_body=json_body,
            content=content,
            headers=headers,
            timeout=timeout,
        )
        try:
            payload = response.json()
            return model.model_validate(payload)
        except (ValueError, ValidationError) as exc:
            raise IsolaError(status=response.status_code, detail="invalid response payload") from exc

    async def request_bytes(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> bytes:
        response = await self.request(method, path, params=params, timeout=timeout)
        return response.content

    async def request_no_content(
        self,
        method: str,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        content: bytes | None = None,
        headers: dict[str, str] | None = None,
        timeout: httpx.Timeout | float | None = DEFAULT_TIMEOUT,
    ) -> None:
        await self.request(method, path, params=params, content=content, headers=headers, timeout=timeout)

    def open_stream(
        self,
        path: str,
        *,
        params: dict[str, Any] | None = None,
        timeout: httpx.Timeout | float | None = None,
    ) -> AbstractAsyncContextManager[httpx.Response]:
        return self._client.stream(
            "GET",
            self.url_for(path),
            params=params,
            timeout=timeout,
        )

    async def raise_for_status(self, response: httpx.Response) -> None:
        if response.status_code < 400:
            return
        body = await response.aread()
        raise error_from_http(response.status_code, response.reason_phrase, body)

    def to_connection_error(self, exc: httpx.RequestError) -> APIConnectionError:
        return connection_error_from_request(exc)

    def url_for(self, path: str) -> str:
        if path.startswith("/"):
            return f"{self.base_url}{path}"
        return f"{self.base_url}/{path}"


def _normalize_base_url(base_url: str) -> str:
    normalized = base_url.strip().rstrip("/")
    if not normalized:
        raise ValueError("base_url must not be empty")
    return normalized


class Isola:
    def __init__(self, *, base_url: str, client: httpx.Client | None = None) -> None:
        self._api = _SyncAPI(base_url, client=client)
        from ._sandbox import Sandboxes

        self.sandboxes = Sandboxes(self._api)

    def close(self) -> None:
        self._api.close()

    def __enter__(self) -> Isola:
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()


class AsyncIsola:
    def __init__(self, *, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._api = _AsyncAPI(base_url, client=client)
        from ._sandbox import AsyncSandboxes

        self.sandboxes = AsyncSandboxes(self._api)

    async def close(self) -> None:
        await self._api.close()

    async def __aenter__(self) -> AsyncIsola:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.close()
