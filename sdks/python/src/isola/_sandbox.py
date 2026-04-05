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
import time
from datetime import datetime
from typing import overload
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._commands import AsyncCommands, Commands
from ._exceptions import IsolaError, IsolaTimeoutError, NotFoundError
from ._filesystem import AsyncFilesystem, Filesystem
from ._models import (
    Container,
    ContainerInfo,
    CreateSandboxPayload,
    ListSandboxesResponse,
    Network,
    PodTemplate,
    ResourceList,
    ResourceRequirements,
    SandboxData,
    SandboxStatus,
    SandboxSummary,
)

_POLL_INTERVAL = 1.0

_TERMINAL_STATUSES = frozenset({SandboxStatus.SUCCEEDED, SandboxStatus.FAILED})


def _sandbox_path(sandbox_id: str) -> str:
    return f"/v1/sandboxes/{quote(sandbox_id, safe='')}"


def _build_resources(
    cpu: float | None, memory: int | None, ephemeral_storage: int | None
) -> ResourceRequirements | None:
    if cpu is None and memory is None and ephemeral_storage is None:
        return None

    resource_list = ResourceList(
        cpu=f"{int(cpu * 1000)}m" if cpu is not None else None,
        memory=f"{memory}Mi" if memory is not None else None,
        ephemeral_storage=f"{ephemeral_storage}Mi" if ephemeral_storage is not None else None,
    )
    return ResourceRequirements(limits=resource_list, requests=resource_list)


def _validate_create_args(
    image: str | None,
    containers: list[Container] | None,
    command: list[str] | None,
    env: dict[str, str] | None,
    cpu: float | None,
    memory: int | None,
    ephemeral_storage: int | None,
    rootfs_snapshot_source: str | None,
) -> list[Container]:
    if containers is not None and image is not None:
        raise ValueError("cannot specify both 'image' and 'containers'")
    if containers is None and image is None:
        raise ValueError("must specify either 'image' or 'containers'")

    if containers is not None:
        per_container = {
            "command": command, "env": env, "cpu": cpu,
            "memory": memory, "ephemeral_storage": ephemeral_storage,
            "rootfs_snapshot_source": rootfs_snapshot_source,
        }
        set_params = [k for k, v in per_container.items() if v is not None]
        if set_params:
            raise ValueError(
                f"cannot specify {', '.join(repr(p) for p in set_params)} "
                f"when using 'containers'; set these on each Container instead"
            )
        return containers

    resources = _build_resources(cpu, memory, ephemeral_storage)
    return [Container(
        image=image,  # type: ignore[arg-type]  # validated non-None above
        command=command,
        env=env,
        resources=resources,
        rootfs_snapshot_source=rootfs_snapshot_source,
    )]


def _check_terminal(sandbox_id: str, status: SandboxStatus) -> None:
    if status in _TERMINAL_STATUSES:
        raise IsolaError(f"sandbox {sandbox_id} reached terminal state: {status.value}")


def _wait_until_running(
    sandbox_id: str,
    api: _SyncAPI,
    max_wait_seconds: int,
) -> SandboxData:
    deadline = time.monotonic() + max_wait_seconds
    while True:
        try:
            data = api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        except NotFoundError as err:
            if time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"sandbox {sandbox_id} did not reach running state within {max_wait_seconds}s"
                ) from err
            time.sleep(_POLL_INTERVAL)
            continue
        if data.status == SandboxStatus.RUNNING:
            return data
        _check_terminal(sandbox_id, data.status)
        if time.monotonic() >= deadline:
            raise IsolaTimeoutError(f"sandbox {sandbox_id} did not reach running state within {max_wait_seconds}s")
        time.sleep(_POLL_INTERVAL)


async def _async_wait_until_running(
    sandbox_id: str,
    api: _AsyncAPI,
    max_wait_seconds: int,
) -> SandboxData:
    deadline = time.monotonic() + max_wait_seconds
    while True:
        try:
            data = await api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        except NotFoundError as err:
            if time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"sandbox {sandbox_id} did not reach running state within {max_wait_seconds}s"
                ) from err
            await asyncio.sleep(_POLL_INTERVAL)
            continue
        if data.status == SandboxStatus.RUNNING:
            return data
        _check_terminal(sandbox_id, data.status)
        if time.monotonic() >= deadline:
            raise IsolaTimeoutError(f"sandbox {sandbox_id} did not reach running state within {max_wait_seconds}s")
        await asyncio.sleep(_POLL_INTERVAL)


class Sandboxes:
    def __init__(self, api: _SyncAPI) -> None:
        self._api = api

    @overload
    def create(
        self,
        *,
        image: str,
        command: list[str] | None = ...,
        env: dict[str, str] | None = ...,
        cpu: float | None = ...,
        memory: int | None = ...,
        ephemeral_storage: int | None = ...,
        rootfs_snapshot_source: str | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        network: Network | None = ...,
        max_wait_seconds: int = ...,
    ) -> Sandbox: ...

    @overload
    def create(
        self,
        *,
        containers: list[Container],
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        network: Network | None = ...,
        max_wait_seconds: int = ...,
    ) -> Sandbox: ...

    def create(
        self,
        *,
        image: str | None = None,
        containers: list[Container] | None = None,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: float | None = None,
        memory: int | None = None,
        ephemeral_storage: int | None = None,
        rootfs_snapshot_source: str | None = None,
        timeout_seconds: int | None = None,
        startup_timeout_seconds: int = 60,
        network: Network | None = None,
        max_wait_seconds: int = 60,
    ) -> Sandbox:
        container_list = _validate_create_args(
            image, containers, command, env, cpu, memory, ephemeral_storage, rootfs_snapshot_source,
        )
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(containers=container_list),
            timeout_seconds=timeout_seconds,
            startup_timeout_seconds=startup_timeout_seconds,
            network=network,
        )

        data = self._api.request_model(
            "POST",
            "/v1/sandboxes",
            SandboxData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        _check_terminal(data.id, data.status)
        if data.status != SandboxStatus.RUNNING and max_wait_seconds != 0:
            data = _wait_until_running(data.id, self._api, max_wait_seconds)
        return Sandbox(self._api, data)

    def list(self) -> list[SandboxSummary]:
        response = self._api.request_model("GET", "/v1/sandboxes", ListSandboxesResponse)
        return response.sandboxes or []

    def get(self, sandbox_id: str) -> Sandbox:
        data = self._api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        return Sandbox(self._api, data)


class AsyncSandboxes:
    def __init__(self, api: _AsyncAPI) -> None:
        self._api = api

    @overload
    async def create(
        self,
        *,
        image: str,
        command: list[str] | None = ...,
        env: dict[str, str] | None = ...,
        cpu: float | None = ...,
        memory: int | None = ...,
        ephemeral_storage: int | None = ...,
        rootfs_snapshot_source: str | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        network: Network | None = ...,
        max_wait_seconds: int = ...,
    ) -> AsyncSandbox: ...

    @overload
    async def create(
        self,
        *,
        containers: list[Container],
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        network: Network | None = ...,
        max_wait_seconds: int = ...,
    ) -> AsyncSandbox: ...

    async def create(
        self,
        *,
        image: str | None = None,
        containers: list[Container] | None = None,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: float | None = None,
        memory: int | None = None,
        ephemeral_storage: int | None = None,
        rootfs_snapshot_source: str | None = None,
        timeout_seconds: int | None = None,
        startup_timeout_seconds: int = 60,
        network: Network | None = None,
        max_wait_seconds: int = 60,
    ) -> AsyncSandbox:
        container_list = _validate_create_args(
            image, containers, command, env, cpu, memory, ephemeral_storage, rootfs_snapshot_source,
        )
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(containers=container_list),
            timeout_seconds=timeout_seconds,
            startup_timeout_seconds=startup_timeout_seconds,
            network=network,
        )

        data = await self._api.request_model(
            "POST",
            "/v1/sandboxes",
            SandboxData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        _check_terminal(data.id, data.status)
        if data.status != SandboxStatus.RUNNING and max_wait_seconds != 0:
            data = await _async_wait_until_running(data.id, self._api, max_wait_seconds)
        return AsyncSandbox(self._api, data)

    async def list(self) -> list[SandboxSummary]:
        response = await self._api.request_model("GET", "/v1/sandboxes", ListSandboxesResponse)
        return response.sandboxes or []

    async def get(self, sandbox_id: str) -> AsyncSandbox:
        data = await self._api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        return AsyncSandbox(self._api, data)


class Sandbox:
    def __init__(self, api: _SyncAPI, data: SandboxData) -> None:
        self._api = api
        self._data = data
        self.commands = Commands(api, data.id)
        self.filesystem = Filesystem(api, data.id)

    @property
    def id(self) -> str:
        return self._data.id

    @property
    def status(self) -> SandboxStatus:
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp

    @property
    def network(self) -> Network | None:
        return self._data.network

    @property
    def timeout_seconds(self) -> int | None:
        return self._data.timeout_seconds

    @property
    def startup_timeout_seconds(self) -> int | None:
        return self._data.startup_timeout_seconds

    @property
    def containers(self) -> list[ContainerInfo]:
        return self._data.pod_template.containers

    def __enter__(self) -> Sandbox:
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.delete()

    def delete(self) -> None:
        self._api.request_no_content("DELETE", _sandbox_path(self._data.id))


class AsyncSandbox:
    def __init__(self, api: _AsyncAPI, data: SandboxData) -> None:
        self._api = api
        self._data = data
        self.commands = AsyncCommands(api, data.id)
        self.filesystem = AsyncFilesystem(api, data.id)

    @property
    def id(self) -> str:
        return self._data.id

    @property
    def status(self) -> SandboxStatus:
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp

    @property
    def network(self) -> Network | None:
        return self._data.network

    @property
    def timeout_seconds(self) -> int | None:
        return self._data.timeout_seconds

    @property
    def startup_timeout_seconds(self) -> int | None:
        return self._data.startup_timeout_seconds

    @property
    def containers(self) -> list[ContainerInfo]:
        return self._data.pod_template.containers

    async def __aenter__(self) -> AsyncSandbox:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.delete()

    async def delete(self) -> None:
        await self._api.request_no_content("DELETE", _sandbox_path(self._data.id))
