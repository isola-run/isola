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

from datetime import datetime
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._commands import AsyncCommands, Commands
from ._filesystem import AsyncFilesystem, Filesystem
from ._models import (
    ContainerSpec,
    CreateSandboxPayload,
    ListSandboxesResponse,
    NetworkSpec,
    PodTemplate,
    ResourceList,
    ResourcesSpec,
    RootfsRestoreSpec,
    SandboxData,
    SandboxStatus,
    SandboxSummary,
)


def _sandbox_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}"


def _build_resources(cpu: str | None, memory: str | None, ephemeral_storage: str | None) -> ResourcesSpec | None:
    if cpu is None and memory is None and ephemeral_storage is None:
        return None

    resource_list = ResourceList(cpu=cpu, memory=memory, ephemeral_storage=ephemeral_storage)
    return ResourcesSpec(limits=resource_list, requests=resource_list)


class Sandboxes:
    def __init__(self, api: _SyncAPI) -> None:
        self._api = api

    def create(
        self,
        *,
        image: str,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: str | None = None,
        memory: str | None = None,
        ephemeral_storage: str | None = None,
        timeout: int | None = None,
        network: NetworkSpec | None = None,
        restore_rootfs_from: RootfsRestoreSpec | None = None,
    ) -> Sandbox:
        resources = _build_resources(cpu, memory, ephemeral_storage)
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(
                container=ContainerSpec(
                    image=image,
                    command=command,
                    env=env,
                    resources=resources,
                )
            ),
            timeout=timeout,
            network=network,
            restore_rootfs_from=restore_rootfs_from,
        )

        data = self._api.request_model(
            "POST",
            "/sandboxes",
            SandboxData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return Sandbox(self._api, data)

    def list(self) -> list[SandboxSummary]:
        response = self._api.request_model("GET", "/sandboxes", ListSandboxesResponse)
        return response.sandboxes or []

    def get(self, sandbox_id: str) -> Sandbox:
        data = self._api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        return Sandbox(self._api, data)


class AsyncSandboxes:
    def __init__(self, api: _AsyncAPI) -> None:
        self._api = api

    async def create(
        self,
        *,
        image: str,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: str | None = None,
        memory: str | None = None,
        ephemeral_storage: str | None = None,
        timeout: int | None = None,
        network: NetworkSpec | None = None,
        restore_rootfs_from: RootfsRestoreSpec | None = None,
    ) -> AsyncSandbox:
        resources = _build_resources(cpu, memory, ephemeral_storage)
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(
                container=ContainerSpec(
                    image=image,
                    command=command,
                    env=env,
                    resources=resources,
                )
            ),
            timeout=timeout,
            network=network,
            restore_rootfs_from=restore_rootfs_from,
        )

        data = await self._api.request_model(
            "POST",
            "/sandboxes",
            SandboxData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return AsyncSandbox(self._api, data)

    async def list(self) -> list[SandboxSummary]:
        response = await self._api.request_model("GET", "/sandboxes", ListSandboxesResponse)
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
    def network(self) -> NetworkSpec | None:
        return self._data.network

    @property
    def timeout(self) -> int | None:
        return self._data.timeout

    @property
    def restore_rootfs_from(self) -> RootfsRestoreSpec | None:
        return self._data.restore_rootfs_from

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
    def network(self) -> NetworkSpec | None:
        return self._data.network

    @property
    def timeout(self) -> int | None:
        return self._data.timeout

    @property
    def restore_rootfs_from(self) -> RootfsRestoreSpec | None:
        return self._data.restore_rootfs_from

    async def __aenter__(self) -> AsyncSandbox:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.delete()

    async def delete(self) -> None:
        await self._api.request_no_content("DELETE", _sandbox_path(self._data.id))
