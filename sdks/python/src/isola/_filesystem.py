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

from typing import BinaryIO
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._models import FileInfo, FileListResult, FileWriteResult, MkdirResult, RenameResult


def _filesystem_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/filesystem"


def _params(path: str, container: str | None = None) -> dict[str, str]:
    params: dict[str, str] = {"path": path}
    if container:
        params["container"] = container
    return params


class Filesystem:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def write(self, path: str, data: bytes | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        return self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=_params(path, container),
            content=data,
            headers={"Content-Type": "application/octet-stream"},
        )

    def read(self, path: str, *, container: str | None = None) -> bytes:
        return self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=_params(path, container))

    def list(self, path: str, *, container: str | None = None) -> list[FileInfo]:
        result = self._api.request_model(
            "GET",
            _filesystem_path(self._sandbox_id) + "/list",
            FileListResult,
            params=_params(path, container),
        )
        return result.entries

    def stat(self, path: str, *, container: str | None = None) -> FileInfo:
        return self._api.request_model(
            "GET",
            _filesystem_path(self._sandbox_id) + "/stat",
            FileInfo,
            params=_params(path, container),
        )

    def exists(self, path: str, *, container: str | None = None) -> bool:
        from ._exceptions import NotFoundError

        try:
            self.stat(path, container=container)
            return True
        except NotFoundError:
            return False

    def mkdir(self, path: str, *, container: str | None = None) -> MkdirResult:
        return self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id) + "/mkdir",
            MkdirResult,
            params=_params(path, container),
        )

    def rename(self, path: str, new_path: str, *, container: str | None = None) -> RenameResult:
        params = _params(path, container)
        params["newPath"] = new_path
        return self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id) + "/rename",
            RenameResult,
            params=params,
        )

    def remove(self, path: str, *, container: str | None = None) -> None:
        self._api.request_no_content("DELETE", _filesystem_path(self._sandbox_id), params=_params(path, container))


class AsyncFilesystem:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def write(self, path: str, data: bytes | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        return await self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=_params(path, container),
            content=data,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def read(self, path: str, *, container: str | None = None) -> bytes:
        return await self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=_params(path, container))

    async def list(self, path: str, *, container: str | None = None) -> list[FileInfo]:
        result = await self._api.request_model(
            "GET",
            _filesystem_path(self._sandbox_id) + "/list",
            FileListResult,
            params=_params(path, container),
        )
        return result.entries

    async def stat(self, path: str, *, container: str | None = None) -> FileInfo:
        return await self._api.request_model(
            "GET",
            _filesystem_path(self._sandbox_id) + "/stat",
            FileInfo,
            params=_params(path, container),
        )

    async def exists(self, path: str, *, container: str | None = None) -> bool:
        from ._exceptions import NotFoundError

        try:
            await self.stat(path, container=container)
            return True
        except NotFoundError:
            return False

    async def mkdir(self, path: str, *, container: str | None = None) -> MkdirResult:
        return await self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id) + "/mkdir",
            MkdirResult,
            params=_params(path, container),
        )

    async def rename(self, path: str, new_path: str, *, container: str | None = None) -> RenameResult:
        params = _params(path, container)
        params["newPath"] = new_path
        return await self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id) + "/rename",
            RenameResult,
            params=params,
        )

    async def remove(self, path: str, *, container: str | None = None) -> None:
        await self._api.request_no_content(
            "DELETE", _filesystem_path(self._sandbox_id), params=_params(path, container)
        )
