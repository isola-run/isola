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
from urllib.parse import quote

from ._client import _AsyncAPI, _SyncAPI
from ._exceptions import IsolaError, IsolaTimeoutError, NotFoundError
from ._models import CreateRootfsSnapshotPayload, RootfsSnapshotData, RootfsSnapshotStatus

_POLL_INTERVAL = 1.0


def _rootfs_snapshot_path(snapshot_id: str) -> str:
    return f"/v1/rootfs-snapshots/{quote(snapshot_id, safe='')}"


def _check_failed(snapshot_id: str, status: RootfsSnapshotStatus) -> None:
    if status == RootfsSnapshotStatus.FAILED:
        raise IsolaError(f"rootfs snapshot {snapshot_id} reached terminal state: {status.value}")


def _wait_until_complete(
    snapshot_id: str,
    api: _SyncAPI,
    max_wait_seconds: int,
) -> RootfsSnapshotData:
    deadline = time.monotonic() + max_wait_seconds
    while True:
        try:
            data = api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        except NotFoundError as err:
            if time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
                ) from err
            time.sleep(_POLL_INTERVAL)
            continue
        if data.status == RootfsSnapshotStatus.SUCCEEDED:
            return data
        _check_failed(snapshot_id, data.status)
        if time.monotonic() >= deadline:
            raise IsolaTimeoutError(
                f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
            )
        time.sleep(_POLL_INTERVAL)


async def _async_wait_until_complete(
    snapshot_id: str,
    api: _AsyncAPI,
    max_wait_seconds: int,
) -> RootfsSnapshotData:
    deadline = time.monotonic() + max_wait_seconds
    while True:
        try:
            data = await api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        except NotFoundError as err:
            if time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
                ) from err
            await asyncio.sleep(_POLL_INTERVAL)
            continue
        if data.status == RootfsSnapshotStatus.SUCCEEDED:
            return data
        _check_failed(snapshot_id, data.status)
        if time.monotonic() >= deadline:
            raise IsolaTimeoutError(
                f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
            )
        await asyncio.sleep(_POLL_INTERVAL)


class RootfsSnapshots:
    """Manage rootfs snapshots.

    Rootfs snapshots capture one container's root filesystem changes at
    a point in time. Other mounts (e.g. tmpfs like /tmp) are not included.
    Restore a snapshot when creating a new sandbox to pick up where you left off.
    """

    def __init__(self, api: _SyncAPI) -> None:
        self._api = api

    def create(
        self,
        *,
        sandbox_id: str,
        snapshot_name: str | None = None,
        container_name: str | None = None,
        timeout_seconds: int = 300,
        ttl_seconds_after_finished: int = 300,
        max_wait_seconds: int = 310,
    ) -> RootfsSnapshot:
        """Create a rootfs snapshot from a running sandbox.

        Blocks until the snapshot completes, up to max_wait_seconds.
        Set max_wait_seconds=0 to return immediately.

        Args:
            sandbox_id: ID of the sandbox to snapshot.
            snapshot_name: Name for the snapshot. Defaults to the
                sandbox's ID on the server. Use this name later as
                rootfs_snapshot_name when creating a new sandbox.
            container_name: Which container to snapshot, for
                multi-container sandboxes. Defaults to the first
                container.
            timeout_seconds: Maximum time for the snapshot operation,
                in seconds. Enforced server-side.
            ttl_seconds_after_finished: How long the Kubernetes resource is
                retained after the snapshot completes, in seconds.
            max_wait_seconds: How long this method polls for completion,
                in seconds. Client-side only.

        Returns:
            A RootfsSnapshot with the snapshot metadata and status.

        Raises:
            IsolaError: If the snapshot fails.
            IsolaTimeoutError: If the snapshot does not complete within
                max_wait_seconds.
        """
        payload = CreateRootfsSnapshotPayload(
            sandbox_id=sandbox_id,
            snapshot_name=snapshot_name,
            container_name=container_name,
            timeout_seconds=timeout_seconds,
            ttl_seconds_after_finished=ttl_seconds_after_finished,
        )
        data = self._api.request_model(
            "POST",
            "/v1/rootfs-snapshots",
            RootfsSnapshotData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        _check_failed(data.id, data.status)
        if data.status != RootfsSnapshotStatus.SUCCEEDED and max_wait_seconds != 0:
            data = _wait_until_complete(data.id, self._api, max_wait_seconds)
        return RootfsSnapshot(data)

    def get(self, snapshot_id: str) -> RootfsSnapshot:
        """Get a rootfs snapshot by ID.

        Args:
            snapshot_id: The snapshot's unique identifier.

        Returns:
            A RootfsSnapshot with the current state.

        Raises:
            NotFoundError: If the snapshot's Kubernetes resource no longer
                exists. A completed snapshot's data remains in storage and
                can still be restored by snapshot_name.
        """
        data = self._api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        return RootfsSnapshot(data)


class AsyncRootfsSnapshots:
    """Async version of RootfsSnapshots."""

    def __init__(self, api: _AsyncAPI) -> None:
        self._api = api

    async def create(
        self,
        *,
        sandbox_id: str,
        snapshot_name: str | None = None,
        container_name: str | None = None,
        timeout_seconds: int = 300,
        ttl_seconds_after_finished: int = 300,
        max_wait_seconds: int = 310,
    ) -> AsyncRootfsSnapshot:
        """Create a rootfs snapshot from a running sandbox.

        Blocks until the snapshot completes, up to max_wait_seconds.
        Set max_wait_seconds=0 to return immediately.

        Args:
            sandbox_id: ID of the sandbox to snapshot.
            snapshot_name: Name for the snapshot. Defaults to the
                sandbox's ID on the server. Use this name later as
                rootfs_snapshot_name when creating a new sandbox.
            container_name: Which container to snapshot, for
                multi-container sandboxes. Defaults to the first
                container.
            timeout_seconds: Maximum time for the snapshot operation,
                in seconds. Enforced server-side.
            ttl_seconds_after_finished: How long the Kubernetes resource is
                retained after the snapshot completes, in seconds.
            max_wait_seconds: How long this method polls for completion,
                in seconds. Client-side only.

        Returns:
            An AsyncRootfsSnapshot with the snapshot metadata and status.

        Raises:
            IsolaError: If the snapshot fails.
            IsolaTimeoutError: If the snapshot does not complete within
                max_wait_seconds.
        """
        payload = CreateRootfsSnapshotPayload(
            sandbox_id=sandbox_id,
            snapshot_name=snapshot_name,
            container_name=container_name,
            timeout_seconds=timeout_seconds,
            ttl_seconds_after_finished=ttl_seconds_after_finished,
        )
        data = await self._api.request_model(
            "POST",
            "/v1/rootfs-snapshots",
            RootfsSnapshotData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        _check_failed(data.id, data.status)
        if data.status != RootfsSnapshotStatus.SUCCEEDED and max_wait_seconds != 0:
            data = await _async_wait_until_complete(data.id, self._api, max_wait_seconds)
        return AsyncRootfsSnapshot(data)

    async def get(self, snapshot_id: str) -> AsyncRootfsSnapshot:
        """Get a rootfs snapshot by ID.

        Args:
            snapshot_id: The snapshot's unique identifier.

        Returns:
            An AsyncRootfsSnapshot with the current state.

        Raises:
            NotFoundError: If the snapshot's Kubernetes resource no longer
                exists. A completed snapshot's data remains in storage and
                can still be restored by snapshot_name.
        """
        data = await self._api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        return AsyncRootfsSnapshot(data)


class RootfsSnapshot:
    """A rootfs snapshot.

    Inspect the snapshot's status and metadata. To restore from this
    snapshot, pass its snapshot_name as rootfs_snapshot_name when
    creating a new sandbox.
    """

    def __init__(self, data: RootfsSnapshotData) -> None:
        self._data = data

    @property
    def id(self) -> str:
        """Unique identifier of the snapshot."""
        return self._data.id

    @property
    def status(self) -> RootfsSnapshotStatus:
        """Current lifecycle status of the snapshot."""
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        """When the snapshot was created."""
        return self._data.creation_timestamp

    @property
    def snapshot_name(self) -> str:
        """Name of the snapshot. Use this to restore from it."""
        return self._data.snapshot_name

    @property
    def sandbox_id(self) -> str:
        """ID of the sandbox this snapshot was taken from."""
        return self._data.sandbox_id

    @property
    def container_name(self) -> str | None:
        """Container that was snapshotted, or None for the default."""
        return self._data.container_name

    @property
    def timeout_seconds(self) -> int | None:
        """Server-side timeout for the snapshot operation, in seconds."""
        return self._data.timeout_seconds

    @property
    def ttl_seconds_after_finished(self) -> int | None:
        """How long the Kubernetes resource is retained after the snapshot completes, in seconds."""
        return self._data.ttl_seconds_after_finished


class AsyncRootfsSnapshot:
    """Async version of RootfsSnapshot."""

    def __init__(self, data: RootfsSnapshotData) -> None:
        self._data = data

    @property
    def id(self) -> str:
        """Unique identifier of the snapshot."""
        return self._data.id

    @property
    def status(self) -> RootfsSnapshotStatus:
        """Current lifecycle status of the snapshot."""
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        """When the snapshot was created."""
        return self._data.creation_timestamp

    @property
    def snapshot_name(self) -> str:
        """Name of the snapshot. Use this to restore from it."""
        return self._data.snapshot_name

    @property
    def sandbox_id(self) -> str:
        """ID of the sandbox this snapshot was taken from."""
        return self._data.sandbox_id

    @property
    def container_name(self) -> str | None:
        """Container that was snapshotted, or None for the default."""
        return self._data.container_name

    @property
    def timeout_seconds(self) -> int | None:
        """Server-side timeout for the snapshot operation, in seconds."""
        return self._data.timeout_seconds

    @property
    def ttl_seconds_after_finished(self) -> int | None:
        """How long the Kubernetes resource is retained after the snapshot completes, in seconds."""
        return self._data.ttl_seconds_after_finished
