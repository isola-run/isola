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


def _filesystem_path(sandbox_id: str) -> str:
    return f"/v1/sandboxes/{quote(sandbox_id, safe='')}/filesystem"


class Filesystem:
    """Read and write files inside a sandbox."""

    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def read_text(self, path: str, *, container: str | None = None) -> str:
        """Read a file from the sandbox as UTF-8 text.

        For binary or non-UTF-8 content, use ``read_bytes`` instead; this
        method raises ``UnicodeDecodeError`` if the file is not valid UTF-8.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            The file's contents decoded as UTF-8.

        Raises:
            UnicodeDecodeError: If the contents are not valid UTF-8.
        """
        return self.read_bytes(path, container=container).decode()

    def read_bytes(self, path: str, *, container: str | None = None) -> bytes:
        """Read a file from the sandbox as raw bytes.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            The file's contents as bytes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)

    def write_text(self, path: str, data: str, *, container: str | None = None) -> None:
        """Write UTF-8 text to a file in the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/hello.txt").
            data: Text to write, encoded as UTF-8.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        self.write_bytes(path, data.encode(), container=container)

    def write_bytes(self, path: str, data: bytes | BinaryIO, *, container: str | None = None) -> None:
        """Write raw bytes to a file in the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/data.bin").
            data: Bytes to write, or any binary file-like object (streamed
                without being read fully into memory).
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        self._api.request_no_content(
            "POST",
            _filesystem_path(self._sandbox_id),
            params=params,
            content=data,
            headers={"Content-Type": "application/octet-stream"},
        )


class AsyncFilesystem:
    """Async version of Filesystem."""

    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def read_text(self, path: str, *, container: str | None = None) -> str:
        """Read a file from the sandbox as UTF-8 text.

        For binary or non-UTF-8 content, use ``read_bytes`` instead; this
        method raises ``UnicodeDecodeError`` if the file is not valid UTF-8.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            The file's contents decoded as UTF-8.

        Raises:
            UnicodeDecodeError: If the contents are not valid UTF-8.
        """
        return (await self.read_bytes(path, container=container)).decode()

    async def read_bytes(self, path: str, *, container: str | None = None) -> bytes:
        """Read a file from the sandbox as raw bytes.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            The file's contents as bytes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return await self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)

    async def write_text(self, path: str, data: str, *, container: str | None = None) -> None:
        """Write UTF-8 text to a file in the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/hello.txt").
            data: Text to write, encoded as UTF-8.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        await self.write_bytes(path, data.encode(), container=container)

    async def write_bytes(self, path: str, data: bytes | BinaryIO, *, container: str | None = None) -> None:
        """Write raw bytes to a file in the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/data.bin").
            data: Bytes to write, or any binary file-like object (streamed
                without being read fully into memory).
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        await self._api.request_no_content(
            "POST",
            _filesystem_path(self._sandbox_id),
            params=params,
            content=data,
            headers={"Content-Type": "application/octet-stream"},
        )
