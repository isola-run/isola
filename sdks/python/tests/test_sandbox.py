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

from isola import AsyncIsola, Isola, NetworkSpec, SandboxStatus


@respx.mock
def test_create_sandbox_maps_flat_resources(sandbox_response_copy: dict[str, object]) -> None:
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
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
            timeout=3600,
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
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.create(image="python:3.12")

    payload = json.loads(create_route.calls[0].request.content)
    assert "resources" not in payload["podTemplate"]["container"]


@respx.mock
def test_list_sandboxes(sandbox_summary_response: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes").mock(return_value=httpx.Response(200, json=sandbox_summary_response))

    with Isola(base_url="http://localhost:8080") as client:
        sandboxes = client.sandboxes.list()

    assert [s.id for s in sandboxes] == ["sandbox-123", "sandbox-456"]


@respx.mock
def test_get_and_delete_sandbox(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

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
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
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
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        with sandbox:
            assert sandbox.id == "sandbox-123"

    assert delete_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_sandbox_context_manager_deletes_on_exit(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.get("sandbox-123")
        async with sandbox:
            assert sandbox.id == "sandbox-123"

    assert delete_route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_create_properties_and_delete(sandbox_response_copy: dict[str, object]) -> None:
    respx.post("http://localhost:8080/sandboxes").mock(return_value=httpx.Response(201, json=sandbox_response_copy))
    delete_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123").mock(return_value=httpx.Response(204))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandbox = await client.sandboxes.create(image="python:3.12", timeout=3600)

        assert sandbox.id == "sandbox-123"
        assert sandbox.status == SandboxStatus.RUNNING
        assert sandbox.creation_timestamp == datetime(2026, 2, 18, tzinfo=timezone.utc)
        assert sandbox.network is not None
        assert sandbox.network.allow_internet_egress is True
        assert sandbox.timeout == 3600

        await sandbox.delete()

    assert delete_route.called


@respx.mock
def test_create_sandbox_with_snapshot(sandbox_response_copy: dict[str, object]) -> None:
    sandbox_response_copy["rootfsSnapshotSources"] = [
        {
            "snapshotKey": "my-snapshot",
            "container": "my-container",
        },
    ]
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(
            image="python:3.12",
            snapshot="my-snapshot",
        )

    assert sandbox.id == "sandbox-123"
    assert sandbox.rootfs_snapshot_sources is not None
    assert len(sandbox.rootfs_snapshot_sources) == 1
    assert sandbox.rootfs_snapshot_sources[0].snapshot_key == "my-snapshot"
    assert sandbox.rootfs_snapshot_sources[0].container == "my-container"

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["rootfsSnapshotSources"] == [
        {
            "snapshotKey": "my-snapshot",
        },
    ]


@respx.mock
def test_create_sandbox_without_snapshot_omits_key(sandbox_response_copy: dict[str, object]) -> None:
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.create(image="python:3.12")

    payload = json.loads(create_route.calls[0].request.content)
    assert "rootfsSnapshotSources" not in payload
