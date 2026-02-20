from __future__ import annotations

from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._models import CommandResult, CommandStatus, CreateCommandPayload
from ._streaming import AsyncCommandOutputStream, CommandOutputStream


def _command_base_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/commands"


def _command_path(sandbox_id: str, command_id: str) -> str:
    return f"{_command_base_path(sandbox_id)}/{quote(command_id, safe='')}"


class Commands:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def run(
        self,
        *,
        cmd: str,
        args: list[str] | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
        text: bool = True,
    ) -> Command:
        params = {"container": container} if container else None
        payload = CreateCommandPayload(cmd=cmd, args=args, env=env, cwd=cwd, timeout=timeout)
        data = self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CommandResult,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return Command(self._api, self._sandbox_id, data.command_id, text=text)


class AsyncCommands:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def run(
        self,
        *,
        cmd: str,
        args: list[str] | None = None,
        env: dict[str, str] | None = None,
        cwd: str | None = None,
        timeout: int | None = None,
        container: str | None = None,
        text: bool = True,
    ) -> AsyncCommand:
        params = {"container": container} if container else None
        payload = CreateCommandPayload(cmd=cmd, args=args, env=env, cwd=cwd, timeout=timeout)
        data = await self._api.request_model(
            "POST",
            _command_base_path(self._sandbox_id),
            CommandResult,
            params=params,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return AsyncCommand(self._api, self._sandbox_id, data.command_id, text=text)


class Command:
    def __init__(self, api: _SyncAPI, sandbox_id: str, command_id: str, *, text: bool = True) -> None:
        self._api = api
        self._sandbox_id = sandbox_id
        self._command_id = command_id
        self._text = text

    @property
    def id(self) -> str:
        return self._command_id

    def stdout(self, *, offset: int = 0, timeout: float | None = None) -> CommandOutputStream:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
        return CommandOutputStream(self._api, path, offset=offset, timeout=timeout, text=self._text)

    def stderr(self, *, offset: int = 0, timeout: float | None = None) -> CommandOutputStream:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
        return CommandOutputStream(self._api, path, offset=offset, timeout=timeout, text=self._text)

    def exit_code(self) -> int | None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return self._api.request_model("GET", path, CommandStatus).exit_code

    def write_stdin(self, data: str | bytes) -> None:
        if isinstance(data, str):
            if not self._text:
                raise TypeError("cannot write str to stdin in binary mode; pass bytes instead")
            raw = data.encode("utf-8")
        else:
            raw = data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    def kill(self) -> None:
        path = _command_path(self._sandbox_id, self._command_id)
        self._api.request_no_content("DELETE", path)


class AsyncCommand:
    def __init__(self, api: _AsyncAPI, sandbox_id: str, command_id: str, *, text: bool = True) -> None:
        self._api = api
        self._sandbox_id = sandbox_id
        self._command_id = command_id
        self._text = text

    @property
    def id(self) -> str:
        return self._command_id

    def stdout(self, *, offset: int = 0, timeout: float | None = None) -> AsyncCommandOutputStream:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdout"
        return AsyncCommandOutputStream(self._api, path, offset=offset, timeout=timeout, text=self._text)

    def stderr(self, *, offset: int = 0, timeout: float | None = None) -> AsyncCommandOutputStream:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stderr"
        return AsyncCommandOutputStream(self._api, path, offset=offset, timeout=timeout, text=self._text)

    async def exit_code(self) -> int | None:
        path = f"{_command_path(self._sandbox_id, self._command_id)}/status"
        return (await self._api.request_model("GET", path, CommandStatus)).exit_code

    async def write_stdin(self, data: str | bytes) -> None:
        if isinstance(data, str):
            if not self._text:
                raise TypeError("cannot write str to stdin in binary mode; pass bytes instead")
            raw = data.encode("utf-8")
        else:
            raw = data
        path = f"{_command_path(self._sandbox_id, self._command_id)}/stdin"
        await self._api.request_no_content(
            "POST",
            path,
            content=raw,
            headers={"Content-Type": "application/octet-stream"},
        )

    async def kill(self) -> None:
        path = _command_path(self._sandbox_id, self._command_id)
        await self._api.request_no_content("DELETE", path)
