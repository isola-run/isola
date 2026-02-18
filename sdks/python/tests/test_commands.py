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
