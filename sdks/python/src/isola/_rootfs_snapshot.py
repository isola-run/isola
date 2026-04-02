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
    max_wait_seconds: int | None,
) -> RootfsSnapshotData:
    deadline = time.monotonic() + max_wait_seconds if max_wait_seconds is not None else None
    while True:
        try:
            data = api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        except NotFoundError as err:
            if deadline is not None and time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
                ) from err
            time.sleep(_POLL_INTERVAL)
            continue
        if data.status == RootfsSnapshotStatus.COMPLETE:
            return data
        _check_failed(snapshot_id, data.status)
        if deadline is not None and time.monotonic() >= deadline:
            raise IsolaTimeoutError(
                f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
            )
        time.sleep(_POLL_INTERVAL)


async def _async_wait_until_complete(
    snapshot_id: str,
    api: _AsyncAPI,
    max_wait_seconds: int | None,
) -> RootfsSnapshotData:
    deadline = time.monotonic() + max_wait_seconds if max_wait_seconds is not None else None
    while True:
        try:
            data = await api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        except NotFoundError as err:
            if deadline is not None and time.monotonic() >= deadline:
                raise IsolaTimeoutError(
                    f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
                ) from err
            await asyncio.sleep(_POLL_INTERVAL)
            continue
        if data.status == RootfsSnapshotStatus.COMPLETE:
            return data
        _check_failed(snapshot_id, data.status)
        if deadline is not None and time.monotonic() >= deadline:
            raise IsolaTimeoutError(
                f"rootfs snapshot {snapshot_id} did not reach complete state within {max_wait_seconds}s"
            )
        await asyncio.sleep(_POLL_INTERVAL)


class RootfsSnapshots:
    def __init__(self, api: _SyncAPI) -> None:
        self._api = api

    def create(
        self,
        *,
        sandbox_id: str,
        snapshot_name: str,
        container_name: str | None = None,
        timeout_seconds: int | None = None,
        ttl_seconds_after_finished: int | None = None,
        max_wait_seconds: int | None = 300,
    ) -> RootfsSnapshot:
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
        if data.status != RootfsSnapshotStatus.COMPLETE and max_wait_seconds != 0:
            data = _wait_until_complete(data.id, self._api, max_wait_seconds)
        return RootfsSnapshot(self._api, data)

    def get(self, snapshot_id: str) -> RootfsSnapshot:
        data = self._api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        return RootfsSnapshot(self._api, data)


class AsyncRootfsSnapshots:
    def __init__(self, api: _AsyncAPI) -> None:
        self._api = api

    async def create(
        self,
        *,
        sandbox_id: str,
        snapshot_name: str,
        container_name: str | None = None,
        timeout_seconds: int | None = None,
        ttl_seconds_after_finished: int | None = None,
        max_wait_seconds: int | None = 300,
    ) -> AsyncRootfsSnapshot:
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
        if data.status != RootfsSnapshotStatus.COMPLETE and max_wait_seconds != 0:
            data = await _async_wait_until_complete(data.id, self._api, max_wait_seconds)
        return AsyncRootfsSnapshot(self._api, data)

    async def get(self, snapshot_id: str) -> AsyncRootfsSnapshot:
        data = await self._api.request_model("GET", _rootfs_snapshot_path(snapshot_id), RootfsSnapshotData)
        return AsyncRootfsSnapshot(self._api, data)


class RootfsSnapshot:
    def __init__(self, api: _SyncAPI, data: RootfsSnapshotData) -> None:
        self._api = api
        self._data = data

    @property
    def id(self) -> str:
        return self._data.id

    @property
    def sandbox_id(self) -> str:
        return self._data.sandbox_id

    @property
    def snapshot_name(self) -> str:
        return self._data.snapshot_name

    @property
    def container_name(self) -> str | None:
        return self._data.container_name

    @property
    def timeout_seconds(self) -> int | None:
        return self._data.timeout_seconds

    @property
    def ttl_seconds_after_finished(self) -> int | None:
        return self._data.ttl_seconds_after_finished

    @property
    def status(self) -> RootfsSnapshotStatus:
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp


class AsyncRootfsSnapshot:
    def __init__(self, api: _AsyncAPI, data: RootfsSnapshotData) -> None:
        self._api = api
        self._data = data

    @property
    def id(self) -> str:
        return self._data.id

    @property
    def sandbox_id(self) -> str:
        return self._data.sandbox_id

    @property
    def snapshot_name(self) -> str:
        return self._data.snapshot_name

    @property
    def container_name(self) -> str | None:
        return self._data.container_name

    @property
    def timeout_seconds(self) -> int | None:
        return self._data.timeout_seconds

    @property
    def ttl_seconds_after_finished(self) -> int | None:
        return self._data.ttl_seconds_after_finished

    @property
    def status(self) -> RootfsSnapshotStatus:
        return self._data.status

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp
