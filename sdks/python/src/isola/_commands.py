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

import asyncio
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._models import CommandResult, CommandStatusResponse, CreateCommandPayload, CreateCommandResponse
from ._streaming import AsyncStreamReader, StreamReader

_LONG_POLL_WAIT_SECONDS = 20  # long poll interval - must stay <= api-gateway's maximum:"25"


def _command_base_path(sandbox_id: str) -> str:
    return f"/v1/sandboxes/{quote(sandbox_id, safe='')}/commands"


def _command_path(sandbox_id: str, command_id: str) -> str:
    return f"{_command_base_path(sandbox_id)}/{quote(command_id, safe='')}"


class Command:
    """A running or completed command inside a sandbox.

    Returned by Commands.spawn(). Use stdout and stderr to stream
    output, wait() to block until completion, or kill() to terminate
    the process.
    """

    def __init__(
        self,
        api: _SyncAPI,
        sandbox_id: str,
        command_id: str,
    ) -> None:
        self._api = api
        self._sandbox_id = sandbox_id
        self._command_id = command_id
        self._stdout: StreamReader | None = None
        self._stderr: StreamReader | None = None

    @property
    def id(self) -> str:
        """Unique identifier of the command."""
        return self._command_id

    @property
    def stdout(self) -> StreamReader:
        """Stream of the command's standard output.

        Yields text chunks as they arrive. Single-use: iterate once
        or call read() to collect everything.
        """
        if self._stdout is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
            self._stdout = StreamReader(self._api, path)
        return self._stdout

    @property
    def stderr(self) -> StreamReader:
        """Stream of the command's standard error.

        Yields text chunks as they arrive. Single-use: iterate once
        or call read() to collect everything.
        """
        if self._stderr is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
            self._stderr = StreamReader(self._api, path)
        return self._stderr

    def exit_code(self) -> int | None:
        """Poll the command's exit status.

        Returns:
            The exit code if the command has finished, or None if it
            is still running.
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return self._api.request_model("GET", path, CommandStatusResponse).exit_code

    def wait(self) -> int:
        """Block until the command finishes.

        Uses long-polling internally, so this does not busy-wait.

        Returns:
            The exit code of the command.
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        while True:
            result = self._api.request_model(
                "GET",
                path,
                CommandStatusResponse,
                params={"waitSeconds": _LONG_POLL_WAIT_SECONDS},
            )
            if result.exit_code is not None:
                return result.exit_code

    def write_stdin(self, data: str | bytes) -> None:
        """Send data to the command's standard input.

        Args:
            data: Text or bytes to write. Strings are encoded as UTF-8.
        """
        raw = data.encode("utf-8") if isinstance(data, str) else data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    def close_stdin(self) -> None:
        """Close the command's standard input.

        Call this after writing all input so the command knows there
        is no more data coming (like pressing Ctrl-D).
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin/close"
        self._api.request_no_content("POST", path)

    def kill(self) -> None:
        """Terminate the command immediately."""
        path = _command_path(self._sandbox_id, self._command_id)
        self._api.request_no_content("DELETE", path)


class AsyncCommand:
    """Async version of Command.

    Returned by AsyncCommands.spawn(). Use stdout and stderr to
    stream output, await wait() to block until completion, or
    await kill() to terminate the process.
    """

    def __init__(
        self,
        api: _AsyncAPI,
        sandbox_id: str,
        command_id: str,
    ) -> None:
        self._api = api
        self._sandbox_id = sandbox_id
        self._command_id = command_id
        self._stdout: AsyncStreamReader | None = None
        self._stderr: AsyncStreamReader | None = None

    @property
    def id(self) -> str:
        """Unique identifier of the command."""
        return self._command_id

    @property
    def stdout(self) -> AsyncStreamReader:
        """Stream of the command's standard output.

        Yields text chunks as they arrive. Single-use: iterate once
        with async for or call await read() to collect everything.
        """
        if self._stdout is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
            self._stdout = AsyncStreamReader(self._api, path)
        return self._stdout

    @property
    def stderr(self) -> AsyncStreamReader:
        """Stream of the command's standard error.

        Yields text chunks as they arrive. Single-use: iterate once
        with async for or call await read() to collect everything.
        """
        if self._stderr is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
            self._stderr = AsyncStreamReader(self._api, path)
        return self._stderr

    async def exit_code(self) -> int | None:
        """Poll the command's exit status.

        Returns:
            The exit code if the command has finished, or None if it
            is still running.
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return (await self._api.request_model("GET", path, CommandStatusResponse)).exit_code

    async def wait(self) -> int:
        """Block until the command finishes.

        Uses long-polling internally, so this does not busy-wait.

        Returns:
            The exit code of the command.
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        while True:
            result = await self._api.request_model(
                "GET",
                path,
                CommandStatusResponse,
                params={"waitSeconds": _LONG_POLL_WAIT_SECONDS},
            )
            if result.exit_code is not None:
                return result.exit_code

    async def write_stdin(self, data: str | bytes) -> None:
        """Send data to the command's standard input.

        Args:
            data: Text or bytes to write. Strings are encoded as UTF-8.
        """
        raw = data.encode("utf-8") if isinstance(data, str) else data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        await self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def close_stdin(self) -> None:
        """Close the command's standard input.

        Call this after writing all input so the command knows there
        is no more data coming (like pressing Ctrl-D).
        """
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin/close"
        await self._api.request_no_content("POST", path)

    async def kill(self) -> None:
        """Terminate the command immediately."""
        path = _command_path(self._sandbox_id, self._command_id)
        await self._api.request_no_content("DELETE", path)


class Commands:
    """Execute commands inside a sandbox."""

    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def spawn(
        self,
        *args: str,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout_seconds: int | None = None,
        container: str | None = None,
    ) -> Command:
        """Start a command without waiting for it to finish.

        Args:
            args: The command and its arguments as separate strings
                (e.g. "ls", "-la").
            env: Environment variables for the command.
            cwd: Working directory inside the sandbox.
            timeout_seconds: Maximum time the command can run, in
                seconds. Enforced server-side. The server kills the
                process if it runs longer. None means no limit.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            A Command handle for streaming output, sending input,
            or waiting for completion.

        Raises:
            ValueError: If no arguments are provided.
        """
        if not args:
            raise ValueError("at least one argument (the command) is required")
        params = {"container": container} if container else None
        payload = CreateCommandPayload(args=list(args), env=env, cwd=cwd, timeout_seconds=timeout_seconds)
        data = self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CreateCommandResponse,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return Command(self._api, self._sandbox_id, data.id)

    def run(
        self,
        *args: str,
        input: str | bytes | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout_seconds: int | None = None,
        container: str | None = None,
    ) -> CommandResult:
        """Run a command and wait for it to complete.

        This is a convenience wrapper around spawn(): it starts the
        command, optionally sends input to stdin, waits for the process
        to exit, and collects stdout and stderr.

        Args:
            args: The command and its arguments as separate strings
                (e.g. "echo", "hello world").
            input: Data to send to the command's stdin. The SDK writes
                this and closes stdin automatically. For interactive
                control, use spawn() with write_stdin() instead.
            env: Environment variables for the command.
            cwd: Working directory inside the sandbox.
            timeout_seconds: Maximum time the command can run, in
                seconds. Enforced server-side. The server kills the
                process if it runs longer. None means no limit.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            A CommandResult with stdout, stderr, and exit_code.

        Raises:
            ValueError: If no arguments are provided.
        """
        cmd = self.spawn(*args, env=env, cwd=cwd, timeout_seconds=timeout_seconds, container=container)
        if input is not None:
            cmd.write_stdin(input)
            cmd.close_stdin()
        exit_code = cmd.wait()
        stdout = cmd.stdout.read()
        stderr = cmd.stderr.read()
        return CommandResult(id=cmd.id, stdout=stdout, stderr=stderr, exit_code=exit_code)


class AsyncCommands:
    """Async version of Commands."""

    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def spawn(
        self,
        *args: str,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout_seconds: int | None = None,
        container: str | None = None,
    ) -> AsyncCommand:
        """Start a command without waiting for it to finish.

        Args:
            args: The command and its arguments as separate strings
                (e.g. "ls", "-la").
            env: Environment variables for the command.
            cwd: Working directory inside the sandbox.
            timeout_seconds: Maximum time the command can run, in
                seconds. Enforced server-side. The server kills the
                process if it runs longer. None means no limit.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            An AsyncCommand handle for streaming output, sending input,
            or waiting for completion.

        Raises:
            ValueError: If no arguments are provided.
        """
        if not args:
            raise ValueError("at least one argument (the command) is required")
        params = {"container": container} if container else None
        payload = CreateCommandPayload(args=list(args), env=env, cwd=cwd, timeout_seconds=timeout_seconds)
        data = await self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CreateCommandResponse,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return AsyncCommand(self._api, self._sandbox_id, data.id)

    async def run(
        self,
        *args: str,
        input: str | bytes | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout_seconds: int | None = None,
        container: str | None = None,
    ) -> CommandResult:
        """Run a command and wait for it to complete.

        This is a convenience wrapper around spawn(): it starts the
        command, optionally sends input to stdin, waits for the process
        to exit, and collects stdout and stderr.

        Args:
            args: The command and its arguments as separate strings
                (e.g. "echo", "hello world").
            input: Data to send to the command's stdin. The SDK writes
                this and closes stdin automatically. For interactive
                control, use spawn() with write_stdin() instead.
            env: Environment variables for the command.
            cwd: Working directory inside the sandbox.
            timeout_seconds: Maximum time the command can run, in
                seconds. Enforced server-side. The server kills the
                process if it runs longer. None means no limit.
            container: Target container name. Only needed for
                multi-container sandboxes.

        Returns:
            A CommandResult with stdout, stderr, and exit_code.

        Raises:
            ValueError: If no arguments are provided.
        """
        cmd = await self.spawn(*args, env=env, cwd=cwd, timeout_seconds=timeout_seconds, container=container)
        if input is not None:
            await cmd.write_stdin(input)
            await cmd.close_stdin()
        stdout, stderr, exit_code = await asyncio.gather(
            cmd.stdout.read(),
            cmd.stderr.read(),
            cmd.wait(),
        )
        return CommandResult(id=cmd.id, stdout=stdout, stderr=stderr, exit_code=exit_code)
