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
from ._exceptions import NotFoundError
from ._models import FilesystemEntry, ListFilesystemEntriesResponse


def _filesystem_path(sandbox_id: str) -> str:
    return f"/v1/sandboxes/{quote(sandbox_id, safe='')}/filesystem"


class Filesystem:
    """Read and write files inside a sandbox."""

    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def write(self, path: str, data: str | bytes | BinaryIO, *, container: str | None = None) -> None:
        """Write a file to the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/hello.txt").
            data: Content to write. Pass a str for text, bytes for
                binary data, or any binary file-like object.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        content: bytes | BinaryIO = data.encode() if isinstance(data, str) else data
        self._api.request_no_content(
            "POST",
            _filesystem_path(self._sandbox_id),
            params=params,
            content=content,
            headers={"Content-Type": "application/octet-stream"},
        )

    def read(self, path: str, *, container: str | None = None) -> bytes:
        """Read a file from the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            File contents as bytes. Decode with .decode() for text.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)

    def list(self, path: str, *, container: str | None = None) -> list[FilesystemEntry]:
        """List directory entries in the sandbox.

        Args:
            path: Absolute path of a directory inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            Metadata for each entry, sorted by name. Symlinks are
            reported, not followed.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        response = self._api.request_model(
            "GET",
            f"{_filesystem_path(self._sandbox_id)}/entries",
            ListFilesystemEntriesResponse,
            params=params,
        )
        return response.entries or []

    def stat(self, path: str, *, container: str | None = None) -> FilesystemEntry:
        """Get metadata for a file, directory, or symlink in the sandbox.

        Symlinks are reported, not followed.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            Entry metadata.

        Raises:
            NotFoundError: If the path does not exist.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return self._api.request_model(
            "GET",
            f"{_filesystem_path(self._sandbox_id)}/stat",
            FilesystemEntry,
            params=params,
        )

    def exists(self, path: str, *, container: str | None = None) -> bool:
        """Check whether a path exists in the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            True if a file, directory, or symlink exists at the path.
        """
        try:
            self.stat(path, container=container)
        except NotFoundError:
            return False
        return True

    def delete(self, path: str, *, recursive: bool = False, container: str | None = None) -> None:
        """Delete a file, empty directory, or symlink from the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            recursive: Delete directories and their contents
                recursively. Required to delete a non-empty directory.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Raises:
            NotFoundError: If the path does not exist.
        """
        params = {"path": path}
        if recursive:
            params["recursive"] = "true"
        if container:
            params["container"] = container

        self._api.request_no_content("DELETE", _filesystem_path(self._sandbox_id), params=params)

    def mkdir(self, path: str, *, container: str | None = None) -> None:
        """Create a directory in the sandbox.

        Missing parent directories are created automatically. Succeeds
        without error if the directory already exists.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        self._api.request_no_content(
            "POST",
            f"{_filesystem_path(self._sandbox_id)}/directories",
            params=params,
        )

    def move(self, source_path: str, destination_path: str, *, container: str | None = None) -> None:
        """Move or rename a file, directory, or symlink in the sandbox.

        Parent directories of the destination are created automatically.
        An existing destination file is overwritten.

        Args:
            source_path: Absolute path to move.
            destination_path: Absolute destination path.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Raises:
            NotFoundError: If the source path does not exist.
        """
        params: dict[str, str] = {}
        if container:
            params["container"] = container

        self._api.request_no_content(
            "POST",
            f"{_filesystem_path(self._sandbox_id)}/move",
            params=params,
            json_body={"sourcePath": source_path, "destinationPath": destination_path},
        )


class AsyncFilesystem:
    """Async version of Filesystem."""

    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def write(self, path: str, data: str | bytes | BinaryIO, *, container: str | None = None) -> None:
        """Write a file to the sandbox.

        Creates the file if it does not exist, overwrites it if it does.
        Parent directories are created automatically.

        Args:
            path: Absolute path inside the sandbox
                (e.g. "/tmp/hello.txt").
            data: Content to write. Pass a str for text, bytes for
                binary data, or any binary file-like object.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        content: bytes | BinaryIO = data.encode() if isinstance(data, str) else data
        await self._api.request_no_content(
            "POST",
            _filesystem_path(self._sandbox_id),
            params=params,
            content=content,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def read(self, path: str, *, container: str | None = None) -> bytes:
        """Read a file from the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            File contents as bytes. Decode with .decode() for text.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return await self._api.request_bytes("GET", _filesystem_path(self._sandbox_id), params=params)

    async def list(self, path: str, *, container: str | None = None) -> list[FilesystemEntry]:
        """List directory entries in the sandbox.

        Args:
            path: Absolute path of a directory inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            Metadata for each entry, sorted by name. Symlinks are
            reported, not followed.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        response = await self._api.request_model(
            "GET",
            f"{_filesystem_path(self._sandbox_id)}/entries",
            ListFilesystemEntriesResponse,
            params=params,
        )
        return response.entries or []

    async def stat(self, path: str, *, container: str | None = None) -> FilesystemEntry:
        """Get metadata for a file, directory, or symlink in the sandbox.

        Symlinks are reported, not followed.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            Entry metadata.

        Raises:
            NotFoundError: If the path does not exist.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        return await self._api.request_model(
            "GET",
            f"{_filesystem_path(self._sandbox_id)}/stat",
            FilesystemEntry,
            params=params,
        )

    async def exists(self, path: str, *, container: str | None = None) -> bool:
        """Check whether a path exists in the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            True if a file, directory, or symlink exists at the path.
        """
        try:
            await self.stat(path, container=container)
        except NotFoundError:
            return False
        return True

    async def delete(self, path: str, *, recursive: bool = False, container: str | None = None) -> None:
        """Delete a file, empty directory, or symlink from the sandbox.

        Args:
            path: Absolute path inside the sandbox.
            recursive: Delete directories and their contents
                recursively. Required to delete a non-empty directory.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Raises:
            NotFoundError: If the path does not exist.
        """
        params = {"path": path}
        if recursive:
            params["recursive"] = "true"
        if container:
            params["container"] = container

        await self._api.request_no_content("DELETE", _filesystem_path(self._sandbox_id), params=params)

    async def mkdir(self, path: str, *, container: str | None = None) -> None:
        """Create a directory in the sandbox.

        Missing parent directories are created automatically. Succeeds
        without error if the directory already exists.

        Args:
            path: Absolute path inside the sandbox.
            container: Target container name. Only needed for
                multi-container sandboxes.
        """
        params = {"path": path}
        if container:
            params["container"] = container

        await self._api.request_no_content(
            "POST",
            f"{_filesystem_path(self._sandbox_id)}/directories",
            params=params,
        )

    async def move(self, source_path: str, destination_path: str, *, container: str | None = None) -> None:
        """Move or rename a file, directory, or symlink in the sandbox.

        Parent directories of the destination are created automatically.
        An existing destination file is overwritten.

        Args:
            source_path: Absolute path to move.
            destination_path: Absolute destination path.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Raises:
            NotFoundError: If the source path does not exist.
        """
        params: dict[str, str] = {}
        if container:
            params["container"] = container

        await self._api.request_no_content(
            "POST",
            f"{_filesystem_path(self._sandbox_id)}/move",
            params=params,
            json_body={"sourcePath": source_path, "destinationPath": destination_path},
        )
