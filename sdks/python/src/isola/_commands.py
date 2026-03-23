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
    """A running or completed command."""

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
        return self._command_id

    @property
    def stdout(self) -> StreamReader:
        if self._stdout is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
            self._stdout = StreamReader(self._api, path)
        return self._stdout

    @property
    def stderr(self) -> StreamReader:
        if self._stderr is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
            self._stderr = StreamReader(self._api, path)
        return self._stderr

    def exit_code(self) -> int | None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return self._api.request_model("GET", path, CommandStatusResponse).exit_code

    def wait(self) -> int:
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
        raw = data.encode("utf-8") if isinstance(data, str) else data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    def close_stdin(self) -> None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin/close"
        self._api.request_no_content("POST", path)

    def kill(self) -> None:
        path = _command_path(self._sandbox_id, self._command_id)
        self._api.request_no_content("DELETE", path)


class AsyncCommand:
    """A running or completed async command."""

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
        return self._command_id

    @property
    def stdout(self) -> AsyncStreamReader:
        if self._stdout is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
            self._stdout = AsyncStreamReader(self._api, path)
        return self._stdout

    @property
    def stderr(self) -> AsyncStreamReader:
        if self._stderr is None:
            path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
            self._stderr = AsyncStreamReader(self._api, path)
        return self._stderr

    async def exit_code(self) -> int | None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return (await self._api.request_model("GET", path, CommandStatusResponse)).exit_code

    async def wait(self) -> int:
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
        raw = data.encode("utf-8") if isinstance(data, str) else data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        await self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def close_stdin(self) -> None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin/close"
        await self._api.request_no_content("POST", path)

    async def kill(self) -> None:
        path = _command_path(self._sandbox_id, self._command_id)
        await self._api.request_no_content("DELETE", path)


class Commands:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def spawn(
        self,
        *args: str,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
    ) -> Command:
        if not args:
            raise ValueError("at least one argument (the command) is required")
        params = {"container": container} if container else None
        payload = CreateCommandPayload(args=list(args), env=env, cwd=cwd, timeout=timeout)
        data = self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CreateCommandResponse,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return Command(self._api, self._sandbox_id, data.command_id)

    def run(
        self,
        *args: str,
        input: str | bytes | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
    ) -> CommandResult:
        cmd = self.spawn(*args, env=env, cwd=cwd, timeout=timeout, container=container)
        if input is not None:
            cmd.write_stdin(input)
            cmd.close_stdin()
        exit_code = cmd.wait()
        stdout = cmd.stdout.read()
        stderr = cmd.stderr.read()
        return CommandResult(command_id=cmd.id, stdout=stdout, stderr=stderr, exit_code=exit_code)


class AsyncCommands:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def spawn(
        self,
        *args: str,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
    ) -> AsyncCommand:
        if not args:
            raise ValueError("at least one argument (the command) is required")
        params = {"container": container} if container else None
        payload = CreateCommandPayload(args=list(args), env=env, cwd=cwd, timeout=timeout)
        data = await self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CreateCommandResponse,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return AsyncCommand(self._api, self._sandbox_id, data.command_id)

    async def run(
        self,
        *args: str,
        input: str | bytes | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
    ) -> CommandResult:
        cmd = await self.spawn(*args, env=env, cwd=cwd, timeout=timeout, container=container)
        if input is not None:
            await cmd.write_stdin(input)
            await cmd.close_stdin()
        stdout, stderr, exit_code = await asyncio.gather(
            cmd.stdout.read(),
            cmd.stderr.read(),
            cmd.wait(),
        )
        return CommandResult(command_id=cmd.id, stdout=stdout, stderr=stderr, exit_code=exit_code)
