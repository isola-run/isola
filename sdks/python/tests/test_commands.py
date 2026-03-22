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

import json

import httpx
import pytest
import respx

from isola import (
    AsyncIsola,
    CommandResult,
    Isola,
)


@respx.mock
def test_spawn_and_command_lifecycle(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    run_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "00000000-0000-0000-0000-000000000001"})
    )
    status_route = respx.get(
        "http://localhost:8080/v1/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001/status"
    ).mock(return_value=httpx.Response(200, json={"exitCode": None}))
    stdin_route = respx.post(
        "http://localhost:8080/v1/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001/stdin"
    ).mock(return_value=httpx.Response(204))
    kill_route = respx.delete(
        "http://localhost:8080/v1/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000001"
    ).mock(return_value=httpx.Response(204))

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn(
            "python",
            "-c",
            "print('hello')",
            env={"DEBUG": "1"},
            cwd="/workspace",
            timeout=30,
            container="worker",
        )
        code = cmd.exit_code()
        cmd.write_stdin(b"input\n")
        cmd.kill()

    assert cmd.id == "00000000-0000-0000-0000-000000000001"
    assert code is None

    run_request = run_route.calls[0].request
    assert run_request.url.params["container"] == "worker"
    assert json.loads(run_request.content) == {
        "args": ["python", "-c", "print('hello')"],
        "env": {"DEBUG": "1"},
        "cwd": "/workspace",
        "timeout": 30,
    }

    assert status_route.called
    assert stdin_route.calls[0].request.content == b"input\n"
    assert kill_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_spawn_and_exit_code(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "00000000-0000-0000-0000-000000000002"})
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/00000000-0000-0000-0000-000000000002/status").mock(
        return_value=httpx.Response(200, json={"exitCode": 0})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("ls", "-la")
        code = await cmd.exit_code()

    assert cmd.id == "00000000-0000-0000-0000-000000000002"
    assert code == 0


@pytest.mark.asyncio
@respx.mock
async def test_async_spawn_stdin_and_kill(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-async-1"})
    )
    stdin_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-async-1/stdin").mock(
        return_value=httpx.Response(204)
    )
    kill_route = respx.delete("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-async-1").mock(
        return_value=httpx.Response(204)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("cat")
        await cmd.write_stdin(b"hello\n")
        await cmd.kill()

    assert stdin_route.calls[0].request.content == b"hello\n"
    assert kill_route.called


@respx.mock
def test_run_returns_command_result(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-1"})
    )
    respx.get(url__regex=r".*/commands/cmd-1/status.*").mock(return_value=httpx.Response(200, json={"exitCode": 0}))
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-1/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: hello world\ndata: \nid: 12\n\n", headers={"content-type": "text/event-stream"}
        )
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-1/stderr").mock(
        return_value=httpx.Response(200, content=b"", headers={"content-type": "text/event-stream"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        result = sandbox.commands.run("echo", "hello world")

    assert isinstance(result, CommandResult)
    assert result.exit_code == 0
    assert result.stdout == "hello world\n"
    assert result.stderr == ""


@respx.mock
def test_command_stdout_streams_text(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-1"})
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-1/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: hello world\ndata: \nid: 12\n\n", headers={"content-type": "text/event-stream"}
        )
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("echo", "hello world")
        output = "".join(cmd.stdout)

    assert output == "hello world\n"


@respx.mock
def test_command_stderr_streams_text(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-2"})
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-2/stderr").mock(
        return_value=httpx.Response(
            200, content=b"data: error output\ndata: \nid: 13\n\n", headers={"content-type": "text/event-stream"}
        )
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("ls", "nonexistent")
        output = "".join(cmd.stderr)

    assert output == "error output\n"


@pytest.mark.asyncio
@respx.mock
async def test_async_command_stdout_streams_text(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-4"})
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-4/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: async output\ndata: \nid: 13\n\n", headers={"content-type": "text/event-stream"}
        )
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("echo", "test")
        chunks_list: list[str] = []
        async for chunk in cmd.stdout:
            chunks_list.append(chunk)

    assert "".join(chunks_list) == "async output\n"


@respx.mock
def test_command_write_stdin_str(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-stdin-text"})
    )
    stdin_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-stdin-text/stdin").mock(
        return_value=httpx.Response(204)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("cat")
        cmd.write_stdin("hello\n")

    assert stdin_route.calls[0].request.content == b"hello\n"


@pytest.mark.asyncio
@respx.mock
async def test_async_command_write_stdin_str(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-async-stdin-text"})
    )
    stdin_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-async-stdin-text/stdin").mock(
        return_value=httpx.Response(204)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("cat")
        await cmd.write_stdin("hello\n")

    assert stdin_route.calls[0].request.content == b"hello\n"


@respx.mock
def test_spawn_minimal_payload(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    run_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-5"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.commands.spawn("ls")

    payload = json.loads(run_route.calls[0].request.content)
    assert payload == {"args": ["ls"]}
    assert "container" not in run_route.calls[0].request.url.params


@respx.mock
def test_spawn_requires_at_least_one_arg(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with pytest.raises(ValueError, match="at least one argument"):
            sandbox.commands.spawn()


@respx.mock
def test_command_exit_code_method(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-ec"})
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-ec/status").mock(
        return_value=httpx.Response(200, json={"exitCode": 42})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("sh", "-c", "exit 42")

        code = cmd.exit_code()
        assert code == 42


@pytest.mark.asyncio
@respx.mock
async def test_async_run_returns_command_result(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-ar"})
    )
    respx.get(url__regex=r".*/commands/cmd-ar/status.*").mock(return_value=httpx.Response(200, json={"exitCode": 0}))
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-ar/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: async result\ndata: \nid: 13\n\n", headers={"content-type": "text/event-stream"}
        )
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-ar/stderr").mock(
        return_value=httpx.Response(200, content=b"", headers={"content-type": "text/event-stream"})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        result = await sandbox.commands.run("echo", "async result")

    assert isinstance(result, CommandResult)
    assert result.exit_code == 0
    assert result.stdout == "async result\n"
    assert result.stderr == ""


@respx.mock
def test_close_stdin_sends_post(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-close"})
    )
    close_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-close/stdin/close").mock(
        return_value=httpx.Response(204)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("cat")
        cmd.close_stdin()

    assert close_route.called


@respx.mock
def test_run_with_input(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-input"})
    )
    stdin_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-input/stdin").mock(
        return_value=httpx.Response(204)
    )
    close_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-input/stdin/close").mock(
        return_value=httpx.Response(204)
    )
    respx.get(url__regex=r".*/commands/cmd-input/status.*").mock(return_value=httpx.Response(200, json={"exitCode": 0}))
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-input/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: hello\ndata: \nid: 6\n\n", headers={"content-type": "text/event-stream"}
        )
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-input/stderr").mock(
        return_value=httpx.Response(200, content=b"", headers={"content-type": "text/event-stream"})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        result = sandbox.commands.run("cat", input="hello\n")

    assert stdin_route.calls[0].request.content == b"hello\n"
    assert close_route.called
    assert result.exit_code == 0
    assert result.stdout == "hello\n"


@pytest.mark.asyncio
@respx.mock
async def test_async_close_stdin_sends_post(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-aclose"})
    )
    close_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-aclose/stdin/close").mock(
        return_value=httpx.Response(204)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("cat")
        await cmd.close_stdin()

    assert close_route.called


def test_command_result_repr() -> None:
    result = CommandResult(command_id="cmd-1", stdout="hello", stderr="world", exit_code=0)
    r = repr(result)
    assert "hello" in r
    assert "world" in r
    assert "exit_code=0" in r


@respx.mock
def test_wait_sends_long_poll_request(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-lp"})
    )
    status_route = respx.get(url__regex=r".*/commands/cmd-lp/status.*").mock(
        return_value=httpx.Response(200, json={"exitCode": 0})
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("echo", "hi")
        code = cmd.wait()

    assert code == 0
    assert status_route.call_count == 1
    assert status_route.calls[0].request.url.params["waitSeconds"] == "20"


@respx.mock
def test_wait_retries_on_null_exit_code(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-retry"})
    )
    status_route = respx.get(url__regex=r".*/commands/cmd-retry/status.*").mock(
        side_effect=[
            httpx.Response(200, json={"exitCode": None}),
            httpx.Response(200, json={"exitCode": 0}),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("sleep", "1")
        code = cmd.wait()

    assert code == 0
    assert status_route.call_count == 2


@pytest.mark.asyncio
@respx.mock
async def test_async_wait_sends_long_poll_request(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"commandId": "cmd-alp"})
    )
    status_route = respx.get(url__regex=r".*/commands/cmd-alp/status.*").mock(
        return_value=httpx.Response(200, json={"exitCode": 0})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        cmd = await sandbox.commands.spawn("echo", "hi")
        code = await cmd.wait()

    assert code == 0
    assert status_route.call_count == 1
    assert status_route.calls[0].request.url.params["waitSeconds"] == "20"
