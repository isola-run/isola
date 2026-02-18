from __future__ import annotations

import io

import httpx
import respx

from isola import Isola


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
