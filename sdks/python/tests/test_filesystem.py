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
from typing import BinaryIO, cast

import httpx
import pytest
import respx

from isola import AsyncIsola, InternalError, Isola, NotFoundError


@respx.mock
def test_filesystem_write_and_read(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )
    read_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"content")
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.write("/workspace/file.txt", b"content", container="worker")
        downloaded = sandbox.filesystem.read("/workspace/file.txt", container="worker")

    assert downloaded == b"content"

    assert write_route.calls[0].request.url.params["path"] == "/workspace/file.txt"
    assert write_route.calls[0].request.url.params["container"] == "worker"
    assert write_route.calls[0].request.content == b"content"

    assert read_route.calls[0].request.url.params["path"] == "/workspace/file.txt"
    assert read_route.calls[0].request.url.params["container"] == "worker"


@respx.mock
def test_filesystem_write_from_file_like(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        file_obj = io.BytesIO(b"print()")
        sandbox.filesystem.write("/workspace/script.py", file_obj)

    assert write_route.calls[0].request.content == b"print()"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_write_and_read(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )
    read_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"data")
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        await sandbox.filesystem.write("/tmp/data.bin", b"data")
        downloaded = await sandbox.filesystem.read("/tmp/data.bin")

    assert downloaded == b"data"

    assert write_route.calls[0].request.url.params["path"] == "/tmp/data.bin"
    assert "container" not in write_route.calls[0].request.url.params
    assert read_route.calls[0].request.url.params["path"] == "/tmp/data.bin"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_with_container(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )
    read_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(200, content=b"abc")
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        await sandbox.filesystem.write("/app/cfg.yaml", b"abc", container="sidecar")
        await sandbox.filesystem.read("/app/cfg.yaml", container="sidecar")

    assert write_route.calls[0].request.url.params["container"] == "sidecar"
    assert read_route.calls[0].request.url.params["container"] == "sidecar"


@respx.mock
def test_filesystem_write_str(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.write("/workspace/hello.py", "print('hello')")

    assert write_route.calls[0].request.content == b"print('hello')"


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_write_str(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        await sandbox.filesystem.write("/workspace/hello.py", "print('hello')")

    assert write_route.calls[0].request.content == b"print('hello')"


class _ChunkedReadValidator(io.BufferedIOBase):
    """File-like object that fails if .read() is ever called without a bounded size."""

    def __init__(self, data: bytes, max_read_size: int) -> None:
        self._buf = io.BytesIO(data)
        self._max_read_size = max_read_size

    def read(self, size: int | None = -1) -> bytes:
        if size is None or size < 0:
            raise AssertionError(
                f"read() called without a bounded size (got {size!r}), which would load the entire file into memory"
            )
        assert size <= self._max_read_size, f"read({size}) exceeds max {self._max_read_size}"
        return self._buf.read(size)

    def readable(self) -> bool:
        return True


@respx.mock
def test_filesystem_write_streams_without_full_buffering(sandbox_response_copy: dict[str, object]) -> None:
    """Verify the SDK passes BinaryIO through to httpx without pre-reading it."""
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(return_value=httpx.Response(204))

    # Payload larger than httpx's 64KB chunk size to ensure multiple reads
    stream = _ChunkedReadValidator(b"x" * 200_000, max_read_size=128 * 1024)

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.write("/big.bin", cast(BinaryIO, stream))


@respx.mock
def test_filesystem_upload_real_file(sandbox_response_copy: dict[str, object], tmp_path: Path) -> None:
    local_file = tmp_path / "script.py"
    local_file.write_bytes(b"print('hello')\n")

    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with open(local_file, "rb") as f:
            sandbox.filesystem.write("/workspace/script.py", f)

    assert write_route.calls[0].request.content == b"print('hello')\n"


# --- Filesystem error handling tests ---


@respx.mock
def test_filesystem_read_raises_on_404(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(404, json={"detail": "file not found"})
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with pytest.raises(NotFoundError) as exc_info:
            sandbox.filesystem.read("/nonexistent/file.txt")

    assert exc_info.value.status_code == 404
    assert "file not found" in exc_info.value.message


@respx.mock
def test_filesystem_read_raises_on_500(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(500, json={"detail": "internal server error"})
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with pytest.raises(InternalError) as exc_info:
            sandbox.filesystem.read("/some/file.txt")

    assert exc_info.value.status_code == 500


@respx.mock
def test_filesystem_write_raises_on_404(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(404, json={"detail": "sandbox not found"})
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with pytest.raises(NotFoundError) as exc_info:
            sandbox.filesystem.write("/workspace/file.txt", b"data")

    assert exc_info.value.status_code == 404


@respx.mock
def test_filesystem_write_raises_on_500(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(500, json={"detail": "disk full"})
    )

    with Isola(url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with pytest.raises(InternalError) as exc_info:
            sandbox.filesystem.write("/workspace/file.txt", b"data")

    assert exc_info.value.status_code == 500
    assert "disk full" in exc_info.value.message


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_read_raises_on_404(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(404, json={"detail": "file not found"})
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        with pytest.raises(NotFoundError):
            await sandbox.filesystem.read("/nonexistent/file.txt")


@pytest.mark.asyncio
@respx.mock
async def test_async_filesystem_write_raises_on_500(
    sandbox_response_copy: dict[str, object], monkeypatch: pytest.MonkeyPatch
) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(500, json={"detail": "disk full"})
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        with pytest.raises(InternalError):
            await sandbox.filesystem.write("/workspace/file.txt", b"data")
