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
    SnapshotRootfs,
    TerminationPolicy,
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
        cpu=f"{round(cpu * 1000)}m" if cpu is not None else None,
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
    rootfs_snapshot_name: str | None,
) -> list[Container]:
    if containers is not None and image is not None:
        raise ValueError("cannot specify both 'image' and 'containers'")
    if containers is None and image is None:
        raise ValueError("must specify either 'image' or 'containers'")

    if containers is not None:
        per_container = {
            "command": command,
            "env": env,
            "cpu": cpu,
            "memory": memory,
            "ephemeral_storage": ephemeral_storage,
            "rootfs_snapshot_name": rootfs_snapshot_name,
        }
        set_params = [k for k, v in per_container.items() if v is not None]
        if set_params:
            raise ValueError(
                f"cannot specify {', '.join(repr(p) for p in set_params)} "
                f"when using 'containers'; set these on each Container instead"
            )
        return containers

    resources = _build_resources(cpu, memory, ephemeral_storage)
    return [
        Container(
            image=image,
            command=command,
            env=env,
            resources=resources,
            rootfs_snapshot_name=rootfs_snapshot_name,
        )
    ]


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
    """Create, list, and retrieve sandboxes."""

    def __init__(self, api: _SyncAPI) -> None:
        self._api = api

    @overload
    def create(
        self,
        *,
        image: str,
        rootfs_snapshot_name: str | None = ...,
        command: list[str] | None = ...,
        env: dict[str, str] | None = ...,
        cpu: float | None = ...,
        memory: int | None = ...,
        ephemeral_storage: int | None = ...,
        network: Network | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        termination_policy: SnapshotRootfs | None = ...,
        max_wait_seconds: int = ...,
    ) -> Sandbox: ...

    @overload
    def create(
        self,
        *,
        containers: list[Container],
        network: Network | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        termination_policy: SnapshotRootfs | None = ...,
        max_wait_seconds: int = ...,
    ) -> Sandbox: ...

    def create(
        self,
        *,
        image: str | None = None,
        rootfs_snapshot_name: str | None = None,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: float | None = None,
        memory: int | None = None,
        ephemeral_storage: int | None = None,
        containers: list[Container] | None = None,
        network: Network | None = None,
        timeout_seconds: int | None = None,
        startup_timeout_seconds: int = 60,
        termination_policy: SnapshotRootfs | None = None,
        max_wait_seconds: int = 65,
    ) -> Sandbox:
        """Create a new sandbox and wait for it to be ready.

        There are two ways to specify what to run:

        Single container (common): pass image and optionally command,
        env, cpu, memory, ephemeral_storage, and rootfs_snapshot_name.

        Multiple containers: pass a containers list of Container objects.
        Per-container options go on each Container.

        The method blocks until the sandbox reaches the Running state,
        up to max_wait_seconds. Set max_wait_seconds=0 to return
        immediately without waiting.

        Timeouts:

        - max_wait_seconds (client-side): How long this method polls
          before giving up. Does not affect the sandbox on the server
        - startup_timeout_seconds (server-side): How long the server
          waits for the sandbox pod to become ready. If it expires,
          the sandbox is marked as Failed.
        - timeout_seconds (server-side, no default): How long the
          sandbox runs before the server begins the termination
          process. None means no limit.

        Args:
            image: Container image to run (e.g. "python:3.12").
                Required unless containers is provided.
            rootfs_snapshot_name: Restore the container's filesystem
                from this named snapshot.
            command: Command and arguments to run in the container.
                If not set, defaults to sleep infinity.
            env: Environment variables as key-value pairs.
            cpu: CPU limit in cores (e.g. 0.5, 2.0). Sets both the
                Kubernetes request and limit. If omitted, no CPU
                limit is applied.
            memory: Memory limit in MiB (e.g. 256, 1024). Sets both
                the Kubernetes request and limit. If omitted, no
                memory limit is applied.
            ephemeral_storage: Ephemeral storage limit in MiB. Sets
                both the Kubernetes request and limit. If omitted,
                no ephemeral storage limit is applied.
            containers: List of Container specs for multi-container
                sandboxes. Cannot be combined with image.
            network: Network policy. Sandboxes have no network access
                by default. See the Network class.
            timeout_seconds: How long the sandbox runs before the
                server begins the termination process, in seconds.
                Enforced server-side. None means no limit.
            startup_timeout_seconds: Maximum time for the sandbox pod
                to become ready, in seconds. Enforced server-side.
            termination_policy: Action to run before the sandbox pod
                is removed. Defaults to immediate deletion if not
                set. Pass a SnapshotRootfs to snapshot the
                container's rootfs changes before removal.
            max_wait_seconds: How long to wait for the sandbox to be
                ready, in seconds. Client-side only. Set to 0 to return
                immediately.

        Returns:
            A Sandbox instance. If max_wait_seconds is 0, the sandbox
            may not be ready yet (check status).

        Raises:
            ValueError: If both image and containers are set, or if
                per-container options are used with containers.
            IsolaTimeoutError: If the sandbox is not ready within
                max_wait_seconds.
            IsolaError: If the sandbox reaches a terminal failed state.
        """
        container_list = _validate_create_args(
            image,
            containers,
            command,
            env,
            cpu,
            memory,
            ephemeral_storage,
            rootfs_snapshot_name,
        )
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(containers=container_list),
            network=network,
            timeout_seconds=timeout_seconds,
            startup_timeout_seconds=startup_timeout_seconds,
            termination_policy=TerminationPolicy(
                type="SnapshotRootfs",
                snapshot_rootfs=termination_policy,
            )
            if termination_policy is not None
            else None,
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
        """List sandboxes.

        Results are eventually consistent.

        Returns:
            A list of SandboxSummary objects with id, status, and
            creation_timestamp.
        """
        response = self._api.request_model("GET", "/v1/sandboxes", ListSandboxesResponse)
        return response.sandboxes or []

    def get(self, sandbox_id: str) -> Sandbox:
        """Get a sandbox by ID.

        Args:
            sandbox_id: The sandbox's unique identifier.

        Returns:
            A Sandbox instance with the current state.

        Raises:
            NotFoundError: If no sandbox with that ID exists.
        """
        data = self._api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        return Sandbox(self._api, data)


class AsyncSandboxes:
    """Async version of Sandboxes."""

    def __init__(self, api: _AsyncAPI) -> None:
        self._api = api

    @overload
    async def create(
        self,
        *,
        image: str,
        rootfs_snapshot_name: str | None = ...,
        command: list[str] | None = ...,
        env: dict[str, str] | None = ...,
        cpu: float | None = ...,
        memory: int | None = ...,
        ephemeral_storage: int | None = ...,
        network: Network | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        termination_policy: SnapshotRootfs | None = ...,
        max_wait_seconds: int = ...,
    ) -> AsyncSandbox: ...

    @overload
    async def create(
        self,
        *,
        containers: list[Container],
        network: Network | None = ...,
        timeout_seconds: int | None = ...,
        startup_timeout_seconds: int = ...,
        termination_policy: SnapshotRootfs | None = ...,
        max_wait_seconds: int = ...,
    ) -> AsyncSandbox: ...

    async def create(
        self,
        *,
        image: str | None = None,
        rootfs_snapshot_name: str | None = None,
        command: list[str] | None = None,
        env: dict[str, str] | None = None,
        cpu: float | None = None,
        memory: int | None = None,
        ephemeral_storage: int | None = None,
        containers: list[Container] | None = None,
        network: Network | None = None,
        timeout_seconds: int | None = None,
        startup_timeout_seconds: int = 60,
        termination_policy: SnapshotRootfs | None = None,
        max_wait_seconds: int = 65,
    ) -> AsyncSandbox:
        """Create a new sandbox and wait for it to be ready.

        There are two ways to specify what to run:

        Single container (common): pass image and optionally command,
        env, cpu, memory, ephemeral_storage, and rootfs_snapshot_name.

        Multiple containers: pass a containers list of Container objects.
        Per-container options go on each Container.

        The method blocks until the sandbox reaches the Running state,
        up to max_wait_seconds. Set max_wait_seconds=0 to return
        immediately without waiting.

        Timeouts:

        - max_wait_seconds (client-side): How long this method polls
          before giving up. Does not affect the sandbox on the server
        - startup_timeout_seconds (server-side): How long the server
          waits for the sandbox pod to become ready. If it expires,
          the sandbox is marked as Failed.
        - timeout_seconds (server-side, no default): How long the
          sandbox runs before the server begins the termination
          process. None means no limit.

        Args:
            image: Container image to run (e.g. "python:3.12").
                Required unless containers is provided.
            rootfs_snapshot_name: Restore the container's filesystem
                from this named snapshot.
            command: Command and arguments to run in the container.
                If not set, defaults to sleep infinity.
            env: Environment variables as key-value pairs.
            cpu: CPU limit in cores (e.g. 0.5, 2.0). Sets both the
                Kubernetes request and limit. If omitted, no CPU
                limit is applied.
            memory: Memory limit in MiB (e.g. 256, 1024). Sets both
                the Kubernetes request and limit. If omitted, no
                memory limit is applied.
            ephemeral_storage: Ephemeral storage limit in MiB. Sets
                both the Kubernetes request and limit. If omitted,
                no ephemeral storage limit is applied.
            containers: List of Container specs for multi-container
                sandboxes. Cannot be combined with image.
            network: Network policy. Sandboxes have no network access
                by default. See the Network class.
            timeout_seconds: How long the sandbox runs before the
                server begins the termination process, in seconds.
                Enforced server-side. None means no limit.
            startup_timeout_seconds: Maximum time for the sandbox pod
                to become ready, in seconds. Enforced server-side.
            termination_policy: Action to run before the sandbox pod
                is removed. Defaults to immediate deletion if not
                set. Pass a SnapshotRootfs to snapshot the
                container's rootfs changes before removal.
            max_wait_seconds: How long to wait for the sandbox to be
                ready, in seconds. Client-side only. Set to 0 to return
                immediately.

        Returns:
            An AsyncSandbox instance. If max_wait_seconds is 0, the
            sandbox may not be ready yet (check status).

        Raises:
            ValueError: If both image and containers are set, or if
                per-container options are used with containers.
            IsolaTimeoutError: If the sandbox is not ready within
                max_wait_seconds.
            IsolaError: If the sandbox reaches a terminal failed state.
        """
        container_list = _validate_create_args(
            image,
            containers,
            command,
            env,
            cpu,
            memory,
            ephemeral_storage,
            rootfs_snapshot_name,
        )
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(containers=container_list),
            network=network,
            timeout_seconds=timeout_seconds,
            startup_timeout_seconds=startup_timeout_seconds,
            termination_policy=TerminationPolicy(
                type="SnapshotRootfs",
                snapshot_rootfs=termination_policy,
            )
            if termination_policy is not None
            else None,
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
        """List sandboxes.

        Results are eventually consistent.

        Returns:
            A list of SandboxSummary objects with id, status, and
            creation_timestamp.
        """
        response = await self._api.request_model("GET", "/v1/sandboxes", ListSandboxesResponse)
        return response.sandboxes or []

    async def get(self, sandbox_id: str) -> AsyncSandbox:
        """Get a sandbox by ID.

        Args:
            sandbox_id: The sandbox's unique identifier.

        Returns:
            An AsyncSandbox instance with the current state.

        Raises:
            NotFoundError: If no sandbox with that ID exists.
        """
        data = await self._api.request_model("GET", _sandbox_path(sandbox_id), SandboxData)
        return AsyncSandbox(self._api, data)


class Sandbox:
    """A running sandbox.

    Use commands to execute processes and filesystem to read and write
    files. Sandboxes are context managers: use with to automatically
    delete the sandbox when you are done.

    Example:

        with client.sandboxes.create(image="alpine:3.21") as sandbox:
            result = sandbox.commands.run("echo", "hello")
            print(result.stdout)
        # sandbox is deleted here
    """

    def __init__(self, api: _SyncAPI, data: SandboxData) -> None:
        self._api = api
        self._data = data
        self.commands = Commands(api, data.id)
        self.filesystem = Filesystem(api, data.id)

    @property
    def id(self) -> str:
        """Unique identifier of the sandbox."""
        return self._data.id

    @property
    def status(self) -> SandboxStatus:
        """Current lifecycle status."""
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        """When the sandbox was created."""
        return self._data.creation_timestamp

    @property
    def network(self) -> Network | None:
        """Network configuration, or None if using defaults."""
        return self._data.network

    @property
    def timeout_seconds(self) -> int | None:
        """How long the sandbox runs before the server begins the termination process, in seconds.

        None means no limit.
        """
        return self._data.timeout_seconds

    @property
    def startup_timeout_seconds(self) -> int | None:
        """Maximum time for the sandbox pod to become ready, in seconds.

        If exceeded, the sandbox is marked as Failed.
        """
        return self._data.startup_timeout_seconds

    @property
    def containers(self) -> list[ContainerInfo]:
        """The sandbox's containers. Does not include init or ephemeral containers."""
        return self._data.pod_template.containers

    def __enter__(self) -> Sandbox:
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.delete()

    def delete(self) -> None:
        """Delete the sandbox.

        Executes the termination policy and removes the pod. Called
        automatically when using the sandbox as a context manager.
        """
        self._api.request_no_content("DELETE", _sandbox_path(self._data.id))


class AsyncSandbox:
    """Async version of Sandbox.

    Use commands to execute processes and filesystem to read and write
    files. Async sandboxes are async context managers: use async with
    to automatically delete the sandbox when you are done.

    Example:

        sandbox = await client.sandboxes.create(image="alpine:3.21")
        async with sandbox:
            result = await sandbox.commands.run("echo", "hello")
            print(result.stdout)
        # sandbox is deleted here
    """

    def __init__(self, api: _AsyncAPI, data: SandboxData) -> None:
        self._api = api
        self._data = data
        self.commands = AsyncCommands(api, data.id)
        self.filesystem = AsyncFilesystem(api, data.id)

    @property
    def id(self) -> str:
        """Unique identifier of the sandbox."""
        return self._data.id

    @property
    def status(self) -> SandboxStatus:
        """Current lifecycle status."""
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        """When the sandbox was created."""
        return self._data.creation_timestamp

    @property
    def network(self) -> Network | None:
        """Network configuration, or None if using defaults."""
        return self._data.network

    @property
    def timeout_seconds(self) -> int | None:
        """How long the sandbox runs before the server begins the termination process, in seconds.

        None means no limit.
        """
        return self._data.timeout_seconds

    @property
    def startup_timeout_seconds(self) -> int | None:
        """Maximum time for the sandbox pod to become ready, in seconds.

        If exceeded, the sandbox is marked as Failed.
        """
        return self._data.startup_timeout_seconds

    @property
    def containers(self) -> list[ContainerInfo]:
        """The sandbox's containers. Does not include init or ephemeral containers."""
        return self._data.pod_template.containers

    async def __aenter__(self) -> AsyncSandbox:
        return self

    async def __aexit__(self, exc_type: object, exc: object, tb: object) -> None:
        await self.delete()

    async def delete(self) -> None:
        """Delete the sandbox.

        Executes the termination policy and removes the pod. Called
        automatically when using the sandbox as an async context manager.
        """
        await self._api.request_no_content("DELETE", _sandbox_path(self._data.id))
