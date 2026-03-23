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
from ._models import FileWriteResult


def _filesystem_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/filesystem"


def _fs_params(path: str, container: str | None) -> dict[str, str]:
    params = {"path": path}
    if container:
        params["container"] = container
    return params


class Filesystem:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def write(self, path: str, data: str | bytes | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        content: bytes | BinaryIO = data.encode() if isinstance(data, str) else data
        return self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=_fs_params(path, container),
            content=content,
            headers={"Content-Type": "application/octet-stream"},
        )

    def read(self, path: str, *, container: str | None = None) -> bytes:
        return self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=_fs_params(path, container))


class AsyncFilesystem:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def write(self, path: str, data: str | bytes | BinaryIO, *, container: str | None = None) -> FileWriteResult:
        content: bytes | BinaryIO = data.encode() if isinstance(data, str) else data
        return await self._api.request_model(
            "POST",
            _filesystem_path(self._sandbox_id),
            FileWriteResult,
            params=_fs_params(path, container),
            content=content,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def read(self, path: str, *, container: str | None = None) -> bytes:
        return await self._api.request_bytes(
            "GET", _filesystem_path(self._sandbox_id), params=_fs_params(path, container)
        )
