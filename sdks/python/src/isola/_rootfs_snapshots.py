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
from ._models import (
    CreateRootfsSnapshotPayload,
    RootfsSnapshotData,
    RootfsSnapshotStatus,
)


def _snapshots_path(sandbox_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/rootfssnapshots"


def _snapshot_path(sandbox_id: str, snapshot_id: str) -> str:
    return f"/sandboxes/{quote(sandbox_id, safe='')}/rootfssnapshots/{quote(snapshot_id, safe='')}"


class RootfsSnapshots:
    def __init__(self, api: _SyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    def create(
        self,
        *,
        snapshot_name: str,
        container: str | None = None,
        active_deadline_seconds: int | None = None,
        ttl_seconds_after_finished: int | None = None,
    ) -> RootfsSnapshot:
        payload = CreateRootfsSnapshotPayload(
            snapshot_name=snapshot_name,
            container=container,
            active_deadline_seconds=active_deadline_seconds,
            ttl_seconds_after_finished=ttl_seconds_after_finished,
        )
        data = self._api.request_model(
            "POST",
            _snapshots_path(self._sandbox_id),
            RootfsSnapshotData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return RootfsSnapshot(self._api, data)

    def get(self, snapshot_id: str) -> RootfsSnapshot:
        data = self._api.request_model(
            "GET",
            _snapshot_path(self._sandbox_id, snapshot_id),
            RootfsSnapshotData,
        )
        return RootfsSnapshot(self._api, data)


class AsyncRootfsSnapshots:
    def __init__(self, api: _AsyncAPI, sandbox_id: str) -> None:
        self._api = api
        self._sandbox_id = sandbox_id

    async def create(
        self,
        *,
        snapshot_name: str,
        container: str | None = None,
        active_deadline_seconds: int | None = None,
        ttl_seconds_after_finished: int | None = None,
    ) -> AsyncRootfsSnapshot:
        payload = CreateRootfsSnapshotPayload(
            snapshot_name=snapshot_name,
            container=container,
            active_deadline_seconds=active_deadline_seconds,
            ttl_seconds_after_finished=ttl_seconds_after_finished,
        )
        data = await self._api.request_model(
            "POST",
            _snapshots_path(self._sandbox_id),
            RootfsSnapshotData,
            json_body=payload.model_dump(by_alias=True, exclude_none=True),
        )
        return AsyncRootfsSnapshot(self._api, data)

    async def get(self, snapshot_id: str) -> AsyncRootfsSnapshot:
        data = await self._api.request_model(
            "GET",
            _snapshot_path(self._sandbox_id, snapshot_id),
            RootfsSnapshotData,
        )
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
    def status(self) -> RootfsSnapshotStatus:
        return self._data.status

    @property
    def failure_message(self) -> str | None:
        return self._data.failure_message

    @property
    def snapshot_key(self) -> str | None:
        return self._data.snapshot_key

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp

    @property
    def start_time(self) -> datetime | None:
        return self._data.start_time

    @property
    def completion_time(self) -> datetime | None:
        return self._data.completion_time


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
    def status(self) -> RootfsSnapshotStatus:
        return self._data.status

    @property
    def failure_message(self) -> str | None:
        return self._data.failure_message

    @property
    def snapshot_key(self) -> str | None:
        return self._data.snapshot_key

    @property
    def creation_timestamp(self) -> datetime:
        return self._data.creation_timestamp

    @property
    def start_time(self) -> datetime | None:
        return self._data.start_time

    @property
    def completion_time(self) -> datetime | None:
        return self._data.completion_time
