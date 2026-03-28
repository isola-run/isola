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

from isola import (
    AsyncIsola,
    Isola,
    IsolaError,
    IsolaTimeoutError,
    NetworkSpec,
    RootfsSnapshotSource,
    SandboxStatus,
)


@respx.mock
def test_create_sandbox_maps_flat_resources(sandbox_response_copy: dict[str, object]) -> None:
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(
            image="python:3.12",
            command=["sleep", "infinity"],
            env={"KEY": "value"},
            cpu="500m",
            memory="1Gi",
            ephemeral_storage="2Gi",
            timeout_seconds=3600,
        )

    assert sandbox.id == "sandbox-123"
    assert sandbox.status == SandboxStatus.RUNNING

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["podTemplate"]["container"]["resources"] == {
        "limits": {"cpu": "500m", "memory": "1Gi", "ephemeralStorage": "2Gi"},
        "requests": {"cpu": "500m", "memory": "1Gi", "ephemeralStorage": "2Gi"},
    }


@respx.mock
def test_create_sandbox_without_resources_omits_key(sandbox_response_copy: dict[str, object]) -> None:
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.create(image="python:3.12")

    payload = json.loads(create_route.calls[0].request.content)
    assert "resources" not in payload["podTemplate"]["container"]


@respx.mock
def test_list_sandboxes(sandbox_summary_response: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json=sandbox_summary_response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandboxes = client.sandboxes.list()

    assert [s.id for s in sandboxes] == ["sandbox-123", "sandbox-456"]


@respx.mock
def test_get_and_delete_sandbox(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/v1/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.delete()

    assert sandbox.id == "sandbox-123"
    assert delete_route.called


@respx.mock
def test_network_spec_acronym_aliases_round_trip(sandbox_response_copy: dict[str, object]) -> None:
    """Verify NetworkSpec fields with acronyms use the correct OpenAPI casing."""
    sandbox_response_copy["network"] = {
        "allowInternetEgress": False,
        "allowClusterDNS": True,
        "allowedEgressCIDRs": ["10.0.0.0/8"],
        "nameservers": ["8.8.8.8"],
    }
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(
            image="python:3.12",
            network=NetworkSpec(
                allow_internet_egress=False,
                allow_cluster_dns=True,
                allowed_egress_cidrs=["10.0.0.0/8"],
                nameservers=["8.8.8.8"],
            ),
        )

    # Response deserialization: server's OpenAPI casing must be parsed correctly
    assert sandbox.network is not None
    assert sandbox.network.allow_cluster_dns is True
    assert sandbox.network.allowed_egress_cidrs == ["10.0.0.0/8"]

    # Request serialization: SDK must send OpenAPI casing
    payload = json.loads(create_route.calls[0].request.content)
    network = payload["network"]
    assert "allowClusterDNS" in network
    assert "allowedEgressCIDRs" in network


@respx.mock
def test_sandbox_context_manager_deletes_on_exit(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/v1/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with sandbox:
            assert sandbox.id == "sandbox-123"

    assert delete_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_sandbox_context_manager_deletes_on_exit(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/v1/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        async with sandbox:
            assert sandbox.id == "sandbox-123"

    assert delete_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_create_properties_and_delete(sandbox_response_copy: dict[str, object]) -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(return_value=httpx.Response(201, json=sandbox_response_copy))
    delete_route = respx.delete("http://localhost:8080/v1/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12", timeout_seconds=3600)

        assert sandbox.id == "sandbox-123"
        assert sandbox.status == SandboxStatus.RUNNING
        assert sandbox.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)
        assert sandbox.network is not None
        assert sandbox.network.allow_internet_egress is True
        assert sandbox.timeout_seconds == 3600

        await sandbox.delete()

    assert delete_route.called


@respx.mock
def test_create_sandbox_with_rootfs_snapshot_source(sandbox_response_copy: dict[str, object]) -> None:
    sandbox_response_copy["rootfsSnapshotSources"] = [{"snapshotName": "my-snapshot"}]
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12", rootfs_snapshot_source="my-snapshot")

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["rootfsSnapshotSources"] == [{"snapshotName": "my-snapshot"}]
    assert sandbox.rootfs_snapshot_sources is not None
    assert len(sandbox.rootfs_snapshot_sources) == 1
    assert sandbox.rootfs_snapshot_sources[0].snapshot_name == "my-snapshot"


@respx.mock
def test_create_sandbox_without_rootfs_snapshot_source_omits_key(
    sandbox_response_copy: dict[str, object],
) -> None:
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.create(image="python:3.12")

    payload = json.loads(create_route.calls[0].request.content)
    assert "rootfsSnapshotSources" not in payload


@respx.mock
def test_rootfs_snapshot_sources_response_deserialization(
    sandbox_response_copy: dict[str, object],
) -> None:
    sandbox_response_copy["rootfsSnapshotSources"] = [
        {"snapshotName": "snap-1"},
        {"snapshotName": "snap-2", "containerName": "worker"},
    ]
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")

    sources = sandbox.rootfs_snapshot_sources
    assert sources is not None
    assert len(sources) == 2
    assert sources[0] == RootfsSnapshotSource(snapshot_name="snap-1")
    assert sources[1] == RootfsSnapshotSource(snapshot_name="snap-2", container_name="worker")


# --- Wait behavior tests ---


def _make_sandbox_response(status: str, sandbox_id: str = "sandbox-123") -> dict[str, object]:
    return {
        "id": sandbox_id,
        "status": status,
        "creationTimestamp": "2026-02-18T00:00:00Z",
        "podTemplate": {"container": {"image": "python:3.12"}},
    }


@respx.mock
def test_create_waits_until_running(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING
    assert get_route.call_count == 2


@respx.mock
def test_create_wait_zero_returns_immediately() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12", max_wait_seconds=0)

    assert sandbox.status == SandboxStatus.CREATING
    assert not get_route.called


@respx.mock
def test_create_wait_zero_raises_on_already_failed() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("failed"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.sandboxes.create(image="python:3.12", max_wait_seconds=0)

    assert not get_route.called


@respx.mock
def test_create_raises_on_failed_during_wait(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=_make_sandbox_response("failed"))
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.sandboxes.create(image="python:3.12")


@respx.mock
def test_create_raises_on_stopped_during_wait(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=_make_sandbox_response("stopped"))
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.sandboxes.create(image="python:3.12")


@respx.mock
def test_create_skips_wait_if_already_running() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("running"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING
    assert not get_route.called


# --- Async wait behavior tests ---


async def _no_sleep(_: float) -> None:
    pass


@pytest.mark.asyncio
@respx.mock
async def test_async_create_waits_until_running(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING
    assert get_route.call_count == 2


@pytest.mark.asyncio
@respx.mock
async def test_async_create_wait_zero_returns_immediately() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12", max_wait_seconds=0)

    assert sandbox.status == SandboxStatus.CREATING
    assert not get_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_create_wait_zero_raises_on_already_failed() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("failed"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaError, match="terminal state"):
            await client.sandboxes.create(image="python:3.12", max_wait_seconds=0)

    assert not get_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_create_raises_on_failed_during_wait(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=_make_sandbox_response("failed"))
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaError, match="terminal state"):
            await client.sandboxes.create(image="python:3.12")


@pytest.mark.asyncio
@respx.mock
async def test_async_create_skips_wait_if_already_running() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("running"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING
    assert not get_route.called


# --- startup_timeout_seconds tests ---


@respx.mock
def test_startup_timeout_seconds_passed_to_api() -> None:
    response = _make_sandbox_response("running")
    response["startupTimeoutSeconds"] = 45
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12", startup_timeout_seconds=45)

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["startupTimeoutSeconds"] == 45
    assert sandbox.startup_timeout_seconds == 45


@respx.mock
def test_startup_timeout_seconds_default_is_60() -> None:
    response = _make_sandbox_response("running")
    response["startupTimeoutSeconds"] = 60
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.create(image="python:3.12")

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["startupTimeoutSeconds"] == 60


@respx.mock
def test_startup_timeout_seconds_none_omits_key() -> None:
    response = _make_sandbox_response("running")
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12", startup_timeout_seconds=None)

    payload = json.loads(create_route.calls[0].request.content)
    assert "startupTimeoutSeconds" not in payload
    assert sandbox.startup_timeout_seconds is None


@respx.mock
def test_wait_raises_when_post_returns_failed() -> None:
    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("failed"))
    )
    get_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123")

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError, match="terminal state"):
        client.sandboxes.create(image="python:3.12")

    assert not get_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_startup_timeout_seconds_passed_to_api() -> None:
    response = _make_sandbox_response("running")
    response["startupTimeoutSeconds"] = 45
    create_route = respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=response)
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12", startup_timeout_seconds=45)

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["startupTimeoutSeconds"] == 45
    assert sandbox.startup_timeout_seconds == 45


# --- Wait eventual consistency tests ---


@respx.mock
def test_wait_tolerates_transient_not_found(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING



@pytest.mark.asyncio
@respx.mock
async def test_async_wait_tolerates_transient_not_found(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(404, json={"detail": "not found"}),
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12")

    assert sandbox.status == SandboxStatus.RUNNING



# --- Wait timeout tests ---


@respx.mock
def test_wait_raises_timeout_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._sandbox.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=_make_sandbox_response("creating"))
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(
        IsolaTimeoutError, match="did not reach running state within 5s"
    ):
        client.sandboxes.create(image="python:3.12", max_wait_seconds=5)


@pytest.mark.asyncio
@respx.mock
async def test_async_wait_raises_timeout_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._sandbox.asyncio.sleep", _no_sleep)

    elapsed = 0.0

    def fake_monotonic() -> float:
        nonlocal elapsed
        elapsed += 2.0
        return elapsed

    monkeypatch.setattr("isola._sandbox.time.monotonic", fake_monotonic)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=_make_sandbox_response("creating"))
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(IsolaTimeoutError, match="did not reach running state within 5s"):
            await client.sandboxes.create(image="python:3.12", max_wait_seconds=5)


@respx.mock
def test_max_wait_seconds_none_means_indefinite(monkeypatch: pytest.MonkeyPatch) -> None:
    """max_wait_seconds=None waits indefinitely (no client-side deadline), relying on server-side timeout."""
    monkeypatch.setattr("isola._sandbox.time.sleep", lambda _: None)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(image="python:3.12", max_wait_seconds=None)

    assert sandbox.status == SandboxStatus.RUNNING


@pytest.mark.asyncio
@respx.mock
async def test_async_max_wait_seconds_none_means_indefinite(monkeypatch: pytest.MonkeyPatch) -> None:
    """max_wait_seconds=None waits indefinitely (async variant)."""
    monkeypatch.setattr("isola._sandbox.asyncio.sleep", _no_sleep)

    respx.post("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(201, json=_make_sandbox_response("creating"))
    )
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        side_effect=[
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("creating")),
            httpx.Response(200, json=_make_sandbox_response("running")),
        ]
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12", max_wait_seconds=None)

    assert sandbox.status == SandboxStatus.RUNNING


def test_timeout_exception_is_isola_error() -> None:
    err = IsolaTimeoutError("timed out")
    assert isinstance(err, IsolaError)
