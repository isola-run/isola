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

from isola import AsyncIsola, Isola, RootfsSnapshotStatus


@respx.mock
def test_create_rootfs_snapshot(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    create_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        snap = sandbox.rootfs_snapshots.create(
            snapshot_name="my-snapshot",
            container="worker",
            active_deadline_seconds=300,
            ttl_seconds_after_finished=300,
        )

    assert snap.id == "mysandbox-my-snapshot-x5k2m"
    assert snap.sandbox_id == "sandbox-123"
    assert snap.snapshot_name == "my-snapshot"
    assert snap.status == RootfsSnapshotStatus.PENDING
    assert create_route.called


@respx.mock
def test_get_rootfs_snapshot(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots/mysandbox-my-snapshot-x5k2m").mock(
        return_value=httpx.Response(200, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        snap = sandbox.rootfs_snapshots.get("mysandbox-my-snapshot-x5k2m")

    assert snap.id == "mysandbox-my-snapshot-x5k2m"
    assert snap.sandbox_id == "sandbox-123"


@respx.mock
def test_create_payload_serialization(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    create_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.rootfs_snapshots.create(
            snapshot_name="my-snapshot",
            container="worker",
            active_deadline_seconds=600,
            ttl_seconds_after_finished=120,
        )

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {
        "snapshotName": "my-snapshot",
        "container": "worker",
        "activeDeadlineSeconds": 600,
        "ttlSecondsAfterFinished": 120,
    }


@respx.mock
def test_create_payload_omits_none_fields(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    create_route = respx.post("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.rootfs_snapshots.create(snapshot_name="my-snapshot")

    payload = json.loads(create_route.calls[0].request.content)
    assert payload == {"snapshotName": "my-snapshot"}
    assert "container" not in payload
    assert "activeDeadlineSeconds" not in payload
    assert "ttlSecondsAfterFinished" not in payload


@respx.mock
def test_rootfs_snapshot_properties(
    sandbox_response_copy: dict[str, object],
) -> None:
    complete_response: dict[str, object] = {
        "id": "snap-id",
        "sandboxId": "sandbox-123",
        "snapshotName": "completed",
        "container": "main",
        "status": "complete",
        "failureMessage": None,
        "snapshotKey": "rootfssnapshots/completed.tar",
        "creationTimestamp": "2026-03-12T00:00:00Z",
        "startTime": "2026-03-12T00:00:05Z",
        "completionTime": "2026-03-12T00:01:00Z",
        "activeDeadlineSeconds": 300,
        "ttlSecondsAfterFinished": 300,
    }
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots/snap-id").mock(
        return_value=httpx.Response(200, json=complete_response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        snap = sandbox.rootfs_snapshots.get("snap-id")

    assert snap.id == "snap-id"
    assert snap.sandbox_id == "sandbox-123"
    assert snap.snapshot_name == "completed"
    assert snap.status == RootfsSnapshotStatus.COMPLETE
    assert snap.failure_message is None
    assert snap.snapshot_key == "rootfssnapshots/completed.tar"
    assert snap.creation_timestamp == datetime(2026, 3, 12, tzinfo=timezone.utc)
    assert snap.start_time == datetime(2026, 3, 12, 0, 0, 5, tzinfo=timezone.utc)
    assert snap.completion_time == datetime(2026, 3, 12, 0, 1, 0, tzinfo=timezone.utc)


@respx.mock
def test_rootfs_snapshot_status_enum_parsing(
    sandbox_response_copy: dict[str, object],
) -> None:
    for status_str, expected in [
        ("pending", RootfsSnapshotStatus.PENDING),
        ("inProgress", RootfsSnapshotStatus.IN_PROGRESS),
        ("complete", RootfsSnapshotStatus.COMPLETE),
        ("failed", RootfsSnapshotStatus.FAILED),
    ]:
        response: dict[str, object] = {
            "id": f"snap-{status_str}",
            "sandboxId": "sandbox-123",
            "snapshotName": "test",
            "status": status_str,
            "creationTimestamp": "2026-03-12T00:00:00Z",
        }
        respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
            return_value=httpx.Response(200, json=sandbox_response_copy)
        )
        respx.get(f"http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots/snap-{status_str}").mock(
            return_value=httpx.Response(200, json=response)
        )

        with Isola(base_url="http://localhost:8080") as client:
            sandbox = client.sandboxes.get("sandbox-123")
            snap = sandbox.rootfs_snapshots.get(f"snap-{status_str}")

        assert snap.status == expected


@pytest.mark.asyncio
@respx.mock
async def test_async_create_rootfs_snapshot(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots").mock(
        return_value=httpx.Response(201, json=rootfs_snapshot_response_copy)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        snap = await sandbox.rootfs_snapshots.create(snapshot_name="my-snapshot")

    assert snap.id == "mysandbox-my-snapshot-x5k2m"
    assert snap.status == RootfsSnapshotStatus.PENDING


@pytest.mark.asyncio
@respx.mock
async def test_async_get_rootfs_snapshot(
    sandbox_response_copy: dict[str, object],
    rootfs_snapshot_response_copy: dict[str, object],
) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.get("http://localhost:8080/sandboxes/sandbox-123/rootfssnapshots/mysandbox-my-snapshot-x5k2m").mock(
        return_value=httpx.Response(200, json=rootfs_snapshot_response_copy)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        snap = await sandbox.rootfs_snapshots.get("mysandbox-my-snapshot-x5k2m")

    assert snap.id == "mysandbox-my-snapshot-x5k2m"
    assert snap.sandbox_id == "sandbox-123"
