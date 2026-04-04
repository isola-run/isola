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
from datetime import datetime, timezone

import httpx
import pytest
import respx

from isola import AsyncIsola, Isola, IsolaError, IsolaTimeoutError, RootfsSnapshotStatus


@respx.mock
def test_create_rootfs_snapshot_with_all_fields(rootfs_snapshot_response_copy: dict[str, object]) -> None:
    rootfs_snapshot_response_copy["ttlSecondsAfterFinished"] = 600
    create_route = respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            container_name="worker",
            timeout_seconds=300,
            ttl_seconds_after_finished=600,
        )

    assert snapshot.id == "snapshot-123"
    assert snapshot.sandbox_id == "sandbox-123"
    assert snapshot.snapshot_name == "my-snapshot"
    assert snapshot.container_name == "worker"
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 600
    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert snapshot.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "containerName": "worker",
        "timeoutSeconds": 300,
        "ttlSecondsAfterFinished": 600,
    }


@respx.mock
def test_create_rootfs_snapshot_with_minimal_fields_sends_defaults(
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    rootfs_snapshot_response_copy.pop("containerName")
    create_route = respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.container_name is None
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 300

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "timeoutSeconds": 300,
        "ttlSecondsAfterFinished": 300,
    }


@respx.mock
def test_get_rootfs_snapshot(rootfs_snapshot_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.get("snapshot-123")

    assert snapshot.id == "snapshot-123"
    assert snapshot.sandbox_id == "sandbox-123"
    assert snapshot.snapshot_name == "my-snapshot"
    assert snapshot.container_name == "worker"
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 300
    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert snapshot.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)


def _make_rootfs_snapshot_response(status: str, snapshot_id: str = "snapshot-123") -> dict[str, object]:
    return {
        "id": snapshot_id,
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "creationTimestamp": "2026-02-18T00:00:00Z",
        "status": status,
    }


@respx.mock
def test_create_waits_until_complete(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress")),
            httpx.Response(200, json=_make_rootfs_snapshot_response("Succeeded")),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert get_route.call_count == 2


@respx.mock
def test_create_wait_zero_returns_immediately() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123")

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            max_wait_seconds=0,
        )

    assert snapshot.status == RootfsSnapshotStatus.PENDING
    assert not get_route.called


@respx.mock
def test_create_wait_zero_raises_on_already_failed() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Failed"))
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            max_wait_seconds=0,
        )


@respx.mock
def test_create_raises_on_failed_during_wait(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=_make_rootfs_snapshot_response("Failed"))
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )


@respx.mock
def test_create_skips_wait_if_already_complete() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Succeeded"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123")

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert not get_route.called


@respx.mock
def test_wait_tolerates_transient_not_found(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress")),
            httpx.Response(200, json=_make_rootfs_snapshot_response("Succeeded")),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        snapshot = client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED


@respx.mock
def test_wait_raises_timeout_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.time.sleep", lambda _: None)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._rootfs_snapshot.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress"))
    )

    with (
        Isola(base_url="http://localhost:8080") as client,
        pytest.raises(IsolaTimeoutError, match="did not reach complete state within 5s"),
    ):
        client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            max_wait_seconds=5,
        )


@respx.mock
def test_wait_raises_timeout_error_when_not_found_persists(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.time.sleep", lambda _: None)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._rootfs_snapshot.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
        ]
    )

    with (
        Isola(base_url="http://localhost:8080") as client,
        pytest.raises(IsolaTimeoutError, match="did not reach complete state within 5s"),
    ):
        client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            max_wait_seconds=5,
        )


async def _no_sleep(_: float) -> None:
    pass


@pytest.mark.asyncio
@respx.mock
async def test_async_create_rootfs_snapshot_with_all_fields(rootfs_snapshot_response_copy: dict[str, object]) -> None:
    rootfs_snapshot_response_copy["ttlSecondsAfterFinished"] = 600
    create_route = respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            container_name="worker",
            timeout_seconds=300,
            ttl_seconds_after_finished=600,
        )

    assert snapshot.id == "snapshot-123"
    assert snapshot.sandbox_id == "sandbox-123"
    assert snapshot.snapshot_name == "my-snapshot"
    assert snapshot.container_name == "worker"
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 600
    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert snapshot.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "containerName": "worker",
        "timeoutSeconds": 300,
        "ttlSecondsAfterFinished": 600,
    }


@pytest.mark.asyncio
@respx.mock
async def test_async_create_rootfs_snapshot_with_minimal_fields_sends_defaults(
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    rootfs_snapshot_response_copy.pop("containerName")
    create_route = respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.container_name is None
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 300

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "timeoutSeconds": 300,
        "ttlSecondsAfterFinished": 300,
    }


@pytest.mark.asyncio
@respx.mock
async def test_async_get_rootfs_snapshot(rootfs_snapshot_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=rootfs_snapshot_response_copy)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.get("snapshot-123")

    assert snapshot.id == "snapshot-123"
    assert snapshot.sandbox_id == "sandbox-123"
    assert snapshot.snapshot_name == "my-snapshot"
    assert snapshot.container_name == "worker"
    assert snapshot.timeout_seconds == 300
    assert snapshot.ttl_seconds_after_finished == 300
    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert snapshot.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)


@pytest.mark.asyncio
@respx.mock
async def test_async_create_waits_until_complete(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress")),
            httpx.Response(200, json=_make_rootfs_snapshot_response("Succeeded")),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert get_route.call_count == 2


@pytest.mark.asyncio
@respx.mock
async def test_async_create_wait_zero_returns_immediately() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123")

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
            max_wait_seconds=0,
        )

    assert snapshot.status == RootfsSnapshotStatus.PENDING
    assert not get_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_create_wait_zero_raises_on_already_failed() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Failed"))
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaError, match="terminal state"):
            await client.rootfs_snapshots.create(
                sandbox_id="sandbox-123",
                snapshot_name="my-snapshot",
                max_wait_seconds=0,
            )


@pytest.mark.asyncio
@respx.mock
async def test_async_create_raises_on_failed_during_wait(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=_make_rootfs_snapshot_response("Failed"))
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaError, match="terminal state"):
            await client.rootfs_snapshots.create(
                sandbox_id="sandbox-123",
                snapshot_name="my-snapshot",
            )


@pytest.mark.asyncio
@respx.mock
async def test_async_create_skips_wait_if_already_complete() -> None:
    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Succeeded"))
    )
    get_route = respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123")

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED
    assert not get_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_wait_tolerates_transient_not_found(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress")),
            httpx.Response(200, json=_make_rootfs_snapshot_response("Succeeded")),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        snapshot = await client.rootfs_snapshots.create(
            sandbox_id="sandbox-123",
            snapshot_name="my-snapshot",
        )

    assert snapshot.status == RootfsSnapshotStatus.SUCCEEDED


@pytest.mark.asyncio
@respx.mock
async def test_async_wait_raises_timeout_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.asyncio.sleep", _no_sleep)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._rootfs_snapshot.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        return_value=httpx.Response(200, json=_make_rootfs_snapshot_response("InProgress"))
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaTimeoutError, match="did not reach complete state within 5s"):
            await client.rootfs_snapshots.create(
                sandbox_id="sandbox-123",
                snapshot_name="my-snapshot",
                max_wait_seconds=5,
            )


@pytest.mark.asyncio
@respx.mock
async def test_async_wait_raises_timeout_error_when_not_found_persists(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr("isola._rootfs_snapshot.asyncio.sleep", _no_sleep)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._rootfs_snapshot.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/rootfs-snapshots").mock(
        return_value=httpx.Response(201, json=_make_rootfs_snapshot_response("Pending"))
    )
    respx.get("http://localhost:8080/v1/rootfs-snapshots/snapshot-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaTimeoutError, match="did not reach complete state within 5s"):
            await client.rootfs_snapshots.create(
                sandbox_id="sandbox-123",
                snapshot_name="my-snapshot",
                max_wait_seconds=5,
            )


