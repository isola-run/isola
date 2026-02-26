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

import io
from pathlib import Path

import httpx
import pytest
import respx

from isola import AsyncIsola, Isola


@respx.mock
def test_filesystem_write_and_read(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/workspace/file.txt", "bytesWritten": 7})
    )
    read_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"content")
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        result = sandbox.filesystem.write("/workspace/file.txt", b"content", container="worker")
        downloaded = sandbox.filesystem.read("/workspace/file.txt", container="worker")

    assert result.absolute_path == "/workspace/file.txt"
    assert result.bytes_written == 7
    assert downloaded == b"content"

    assert write_route.calls[0].request.url.params["path"] == "/workspace/file.txt"
    assert write_route.calls[0].request.url.params["container"] == "worker"
    assert write_route.calls[0].request.content == b"content"

    assert read_route.calls[0].request.url.params["path"] == "/workspace/file.txt"
    assert read_route.calls[0].request.url.params["container"] == "worker"


@respx.mock
def test_filesystem_write_from_file_like(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/workspace/script.py", "bytesWritten": 6})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        file_obj = io.BytesIO(b"print()")
        sandbox.filesystem.write("/workspace/script.py", file_obj)

    assert write_route.calls[0].request.content == b"print()"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_write_and_read(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/tmp/data.bin", "bytesWritten": 4})
    )
    read_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"data")
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        result = await sandbox.filesystem.write("/tmp/data.bin", b"data")
        downloaded = await sandbox.filesystem.read("/tmp/data.bin")

    assert result.absolute_path == "/tmp/data.bin"
    assert result.bytes_written == 4
    assert downloaded == b"data"

    assert write_route.calls[0].request.url.params["path"] == "/tmp/data.bin"
    assert "container" not in write_route.calls[0].request.url.params
    assert read_route.calls[0].request.url.params["path"] == "/tmp/data.bin"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_with_container(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/app/cfg.yaml", "bytesWritten": 3})
    )
    read_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"abc")
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        await sandbox.filesystem.write("/app/cfg.yaml", b"abc", container="sidecar")
        await sandbox.filesystem.read("/app/cfg.yaml", container="sidecar")

    assert write_route.calls[0].request.url.params["container"] == "sidecar"
    assert read_route.calls[0].request.url.params["container"] == "sidecar"


def test_filesystem_write_from_str_file_like_raises_type_error(sandbox_response_copy: dict[str, object]) -> None:
    with Isola(base_url="http://localhost:8080") as client:
        # Manually construct a sandbox to avoid HTTP call
        from isola._models import SandboxData

        data = SandboxData.model_validate(sandbox_response_copy)
        from isola._sandbox import Sandbox

        sandbox = Sandbox(client._api, data)

        with pytest.raises(TypeError):
            sandbox.filesystem.write("/tmp/file.txt", io.StringIO("text"))


class _ChunkedReadValidator(io.RawIOBase):
    """File-like object that fails if .read() is ever called without a bounded size."""

    def __init__(self, data: bytes, max_read_size: int) -> None:
        self._buf = io.BytesIO(data)
        self._max_read_size = max_read_size

    def read(self, size: int = -1) -> bytes:  # type: ignore[override]
        if size is None or size < 0:
            raise AssertionError(
                f"read() called without a bounded size (got {size!r}), which would load the entire file into memory"
            )
        assert size <= self._max_read_size, f"read({size}) exceeds max {self._max_read_size}"
        return self._buf.read(size)

    def readinto(self, b: bytearray) -> int:
        data = self._buf.read(len(b))
        n = len(data)
        b[:n] = data
        return n

    def readable(self) -> bool:
        return True


@respx.mock
def test_filesystem_write_streams_without_full_buffering(sandbox_response_copy: dict[str, object]) -> None:
    """Verify the SDK passes BinaryIO through to httpx without pre-reading it."""
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/big.bin", "bytesWritten": 200_000})
    )

    # Payload larger than httpx's 64KB chunk size to ensure multiple reads
    stream = _ChunkedReadValidator(b"x" * 200_000, max_read_size=128 * 1024)

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.write("/big.bin", stream)


@respx.mock
def test_filesystem_upload_real_file(sandbox_response_copy: dict[str, object], tmp_path: Path) -> None:
    local_file = tmp_path / "script.py"
    local_file.write_bytes(b"print('hello')\n")

    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/workspace/script.py", "bytesWritten": 15})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with open(local_file, "rb") as f:
            result = sandbox.filesystem.write("/workspace/script.py", f)

    assert result.bytes_written == 15
    assert write_route.calls[0].request.content == b"print('hello')\n"


@respx.mock
def test_filesystem_download_to_real_file(sandbox_response_copy: dict[str, object], tmp_path: Path) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"downloaded content")
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        data = sandbox.filesystem.read("/workspace/output.txt")

    dest = tmp_path / "output.txt"
    dest.write_bytes(data)

    assert dest.read_bytes() == b"downloaded content"


@respx.mock
def test_filesystem_list(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    list_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/list").mock(
        return_value=httpx.Response(
            200,
            json={
                "entries": [
                    {"name": "a.txt", "path": "/workspace/a.txt", "isDir": False, "size": 10, "mode": "-rw-r--r--"},
                    {"name": "subdir", "path": "/workspace/subdir", "isDir": True, "size": 0, "mode": "drwxr-xr-x"},
                ]
            },
        )
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        entries = sandbox.filesystem.list("/workspace")

    assert len(entries) == 2
    assert entries[0].name == "a.txt"
    assert entries[0].is_dir is False
    assert entries[0].size == 10
    assert entries[1].name == "subdir"
    assert entries[1].is_dir is True
    assert list_route.calls[0].request.url.params["path"] == "/workspace"


@respx.mock
def test_filesystem_stat(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    stat_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/stat").mock(
        return_value=httpx.Response(
            200,
            json={"name": "file.txt", "path": "/workspace/file.txt", "isDir": False, "size": 42, "mode": "-rw-r--r--"},
        )
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        info = sandbox.filesystem.stat("/workspace/file.txt")

    assert info.name == "file.txt"
    assert info.path == "/workspace/file.txt"
    assert info.size == 42
    assert info.is_dir is False
    assert stat_route.calls[0].request.url.params["path"] == "/workspace/file.txt"


@respx.mock
def test_filesystem_exists_true(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/stat").mock(
        return_value=httpx.Response(
            200,
            json={"name": "file.txt", "path": "/workspace/file.txt", "isDir": False, "size": 1, "mode": "-rw-r--r--"},
        )
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        assert sandbox.filesystem.exists("/workspace/file.txt") is True


@respx.mock
def test_filesystem_exists_false(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/stat").mock(
        return_value=httpx.Response(404, json={"status": 404, "detail": "path not found"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        assert sandbox.filesystem.exists("/workspace/nope.txt") is False


@respx.mock
def test_filesystem_mkdir(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    mkdir_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem/mkdir").mock(
        return_value=httpx.Response(201, json={"absolutePath": "/workspace/newdir"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        result = sandbox.filesystem.mkdir("/workspace/newdir")

    assert result.absolute_path == "/workspace/newdir"
    assert mkdir_route.calls[0].request.url.params["path"] == "/workspace/newdir"


@respx.mock
def test_filesystem_rename(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    rename_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/filesystem/rename").mock(
        return_value=httpx.Response(200, json={"absolutePath": "/workspace/new-name.txt"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        result = sandbox.filesystem.rename("/workspace/old.txt", "/workspace/new-name.txt")

    assert result.absolute_path == "/workspace/new-name.txt"
    assert rename_route.calls[0].request.url.params["path"] == "/workspace/old.txt"
    assert rename_route.calls[0].request.url.params["newPath"] == "/workspace/new-name.txt"


@respx.mock
def test_filesystem_remove(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    remove_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.remove("/workspace/remove-me.txt")

    assert remove_route.calls[0].request.url.params["path"] == "/workspace/remove-me.txt"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_list(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/list").mock(
        return_value=httpx.Response(
            200,
            json={"entries": [{"name": "f.txt", "path": "/f.txt", "isDir": False, "size": 5, "mode": "-rw-r--r--"}]},
        )
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        entries = await sandbox.filesystem.list("/")

    assert len(entries) == 1
    assert entries[0].name == "f.txt"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_exists(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/filesystem/stat").mock(
        return_value=httpx.Response(404, json={"status": 404, "detail": "not found"})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        assert await sandbox.filesystem.exists("/nope") is False


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_remove(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.delete("http://localhost:8080/sandboxes/sandbox-123/filesystem").mock(return_value=httpx.Response(204))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        await sandbox.filesystem.remove("/tmp/gone")
