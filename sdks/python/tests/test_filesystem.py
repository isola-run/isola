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
