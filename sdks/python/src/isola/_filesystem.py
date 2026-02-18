from __future__ import annotations

from typing import BinaryIO
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._models import FileWriteResult

BytesLike = bytes | bytearray | memoryview


def _filesystem_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/filesystem"


def _coerce_bytes(data: BytesLike | BinaryIO) -> bytes:
    if isinstance(data, (bytes, bytearray, memoryview)):
        return bytes(data)

    body = data.read()
    if isinstance(body, bytes):
        return body
    raise TypeError("file-like object must return bytes")


class Filesystem:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def write(self, path: str, data: BytesLike | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        params = {"path": path}
        if container:
            params["container"] = container

        result = self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=params,
            content=_coerce_bytes(data),
            headers={"Content-Type": "application/octet-stream"},
        )
        return result

    def read(self, path: str, *, container: str | None = None) -> bytes:
        params = {"path": path}
        if container:
            params["container"] = container

        return self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)


class AsyncFilesystem:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def write(self, path: str, data: BytesLike | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        params = {"path": path}
        if container:
            params["container"] = container

        result = await self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=params,
            content=_coerce_bytes(data),
            headers={"Content-Type": "application/octet-stream"},
        )
        return result

    async def read(self, path: str, *, container: str | None = None) -> bytes:
        params = {"path": path}
        if container:
            params["container"] = container

        return await self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)
