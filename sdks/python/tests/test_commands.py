from __future__ import annotations

import json

import httpx
import pytest
import respx

from isola import AsyncIsola, Isola


@respx.mock
def test_command_lifecycle(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    run_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "00000000-0000-0000-0000-000000000001"})
    )
    status_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001/status").mock(
        return_value=httpx.Response(200, json={"exitCode": None})
    )
    stdin_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001/stdin").mock(
        return_value=httpx.Response(204)
    )
    kill_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001").mock(
        return_value=httpx.Response(204)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.run(
            cmd="python",
            args=["-c", "print('hello')"],
            env={"DEBUG": "1"},
            cwd="/workspace",
            timeout=30,
            container="worker",
        )
        status = cmd.get_status()
        cmd.write_stdin(b"input\n")
        cmd.kill()

    assert cmd.id == "00000000-0000-0000-0000-000000000001"
    assert status.exit_code is None

    run_request = run_route.calls[0].request
    assert run_request.url.params["container"] == "worker"
    assert json.loads(run_request.content) == {
        "cmd": "python",
        "args": ["-c", "print('hello')"],
        "env": {"DEBUG": "1"},
        "cwd": "/workspace",
        "timeout": 30,
    }

    assert status_route.called
    assert stdin_route.calls[0].request.content == b"input\n"
    assert kill_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_command_run_and_status(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "00000000-0000-0000-0000-000000000002"})
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000002/status").mock(
        return_value=httpx.Response(200, json={"exitCode": 0})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.run(cmd="ls", args=["-la"])
        status = await cmd.get_status()

    assert cmd.id == "00000000-0000-0000-0000-000000000002"
    assert status.exit_code == 0


@pytest.mark.asyncio
@respx.mock
async def test_async_command_stdin_and_kill(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-async-1"})
    )
    stdin_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-async-1/stdin").mock(
        return_value=httpx.Response(204)
    )
    kill_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-async-1").mock(
        return_value=httpx.Response(204)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.run(cmd="cat")
        await cmd.write_stdin(b"hello\n")
        await cmd.kill()

    assert stdin_route.calls[0].request.content == b"hello\n"
    assert kill_route.called


@respx.mock
def test_command_stdout_streams_bytes(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-1"})
    )
    stdout_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-1/stdout").mock(
        return_value=httpx.Response(200, content=b"hello world\n")
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.run(cmd="echo", args=["hello world"])
        with cmd.stdout() as chunks:
            output = b"".join(chunks)

    assert output == b"hello world\n"
    assert stdout_route.calls[0].request.url.params["offset"] == "0"


@respx.mock
def test_command_stderr_streams_bytes(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-2"})
    )
    stderr_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-2/stderr").mock(
        return_value=httpx.Response(200, content=b"error output\n")
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.run(cmd="ls", args=["nonexistent"])
        with cmd.stderr() as chunks:
            output = b"".join(chunks)

    assert output == b"error output\n"
    assert stderr_route.calls[0].request.url.params["offset"] == "0"


@respx.mock
def test_command_stdout_with_offset(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-3"})
    )
    stdout_route = respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-3/stdout").mock(
        return_value=httpx.Response(200, content=b"resumed")
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.run(cmd="cat", args=["log.txt"])
        with cmd.stdout(offset=100) as chunks:
            output = b"".join(chunks)

    assert output == b"resumed"
    assert stdout_route.calls[0].request.url.params["offset"] == "100"


@pytest.mark.asyncio
@respx.mock
async def test_async_command_stdout_streams_bytes(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-4"})
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/commands/cmd-4/stdout").mock(
        return_value=httpx.Response(200, content=b"async output\n")
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.run(cmd="echo", args=["test"])
        chunks_list: list[bytes] = []
        async with cmd.stdout() as chunks:
            async for chunk in chunks:
                chunks_list.append(chunk)

    assert b"".join(chunks_list) == b"async output\n"


@respx.mock
def test_command_run_minimal_payload(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    run_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-5"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.commands.run(cmd="ls")

    payload = json.loads(run_route.calls[0].request.content)
    assert payload == {"cmd": "ls"}
    assert "container" not in run_route.calls[0].request.url.params
