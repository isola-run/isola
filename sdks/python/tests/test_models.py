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

from isola._models import (
    Container,
    CreateSandboxPayload,
    ListSandboxesResponse,
    NetworkSpec,
    PodTemplate,
    ResourceList,
    ResourceRequirements,
    SandboxData,
    SandboxStatus,
)

# --- NetworkSpec manual aliases (override to_camel for acronyms) ---


class TestNetworkSpecAliases:
    def test_allow_cluster_dns_alias(self) -> None:
        net = NetworkSpec(allow_cluster_dns=True)
        dumped = net.model_dump(by_alias=True, exclude_none=True)
        assert "allowClusterDNS" in dumped
        assert dumped["allowClusterDNS"] is True
        # Standard camelCase would produce "allowClusterDns" -- verify that does NOT appear
        assert "allowClusterDns" not in dumped

    def test_allowed_egress_cidrs_alias(self) -> None:
        net = NetworkSpec(allowed_egress_cidrs=["10.0.0.0/8"])
        dumped = net.model_dump(by_alias=True, exclude_none=True)
        assert "allowedEgressCIDRs" in dumped
        assert dumped["allowedEgressCIDRs"] == ["10.0.0.0/8"]
        # Standard camelCase would produce "allowedEgressCidrs"
        assert "allowedEgressCidrs" not in dumped

    def test_allow_internet_egress_uses_standard_camel(self) -> None:
        net = NetworkSpec(allow_internet_egress=True)
        dumped = net.model_dump(by_alias=True, exclude_none=True)
        assert "allowInternetEgress" in dumped

    def test_nameservers_no_alias_change(self) -> None:
        net = NetworkSpec(nameservers=["8.8.8.8"])
        dumped = net.model_dump(by_alias=True, exclude_none=True)
        assert "nameservers" in dumped


# --- Validation by both name and alias ---


class TestValidateByNameAndAlias:
    def test_construct_with_snake_case(self) -> None:
        spec = Container(image="ubuntu:22.04")
        assert spec.image == "ubuntu:22.04"

    def test_construct_with_camel_case(self) -> None:
        payload = CreateSandboxPayload.model_validate(
            {
                "podTemplate": {"containers": [{"image": "node:20"}]},
                "timeoutSeconds": 300,
                "startupTimeoutSeconds": 60,
            }
        )
        assert payload.pod_template.containers[0].image == "node:20"
        assert payload.timeout_seconds == 300
        assert payload.startup_timeout_seconds == 60

    def test_construct_with_snake_case_dict(self) -> None:
        payload = CreateSandboxPayload.model_validate(
            {
                "pod_template": {"containers": [{"image": "node:20"}]},
                "timeout_seconds": 300,
                "startup_timeout_seconds": 60,
            }
        )
        assert payload.pod_template.containers[0].image == "node:20"
        assert payload.timeout_seconds == 300
        assert payload.startup_timeout_seconds == 60

    def test_network_spec_by_alias(self) -> None:
        net = NetworkSpec.model_validate(
            {
                "allowClusterDNS": True,
                "allowedEgressCIDRs": ["10.0.0.0/8"],
            }
        )
        assert net.allow_cluster_dns is True
        assert net.allowed_egress_cidrs == ["10.0.0.0/8"]

    def test_network_spec_by_name(self) -> None:
        net = NetworkSpec.model_validate(
            {
                "allow_cluster_dns": False,
                "allowed_egress_cidrs": ["192.168.0.0/16"],
            }
        )
        assert net.allow_cluster_dns is False
        assert net.allowed_egress_cidrs == ["192.168.0.0/16"]


# --- Round-trip serialization ---


class TestRoundTrip:
    def test_sandbox_data_round_trip(self) -> None:
        camel_json = {
            "id": "sb-42",
            "status": "running",
            "creationTimestamp": "2026-03-15T12:30:00Z",
            "podTemplate": {
                "containers": [{
                    "image": "python:3.12",
                    "command": ["sleep", "infinity"],
                    "resources": {
                        "limits": {"cpu": "1", "memory": "2Gi", "ephemeralStorage": "5Gi"},
                        "requests": {"cpu": "500m", "memory": "1Gi"},
                    },
                }]
            },
            "timeoutSeconds": 3600,
            "network": {
                "allowInternetEgress": True,
                "allowClusterDNS": False,
                "allowedEgressCIDRs": ["10.0.0.0/8"],
                "nameservers": ["8.8.8.8"],
            },
            "rootfsSnapshotSources": [
                {"snapshotName": "snap-1", "containerName": "main"},
            ],
        }

        model = SandboxData.model_validate(camel_json)

        # Verify parsing
        assert model.id == "sb-42"
        assert model.status is SandboxStatus.RUNNING
        assert model.timeout_seconds == 3600
        assert model.network is not None
        assert model.network.allow_internet_egress is True
        assert model.network.allow_cluster_dns is False
        assert model.network.allowed_egress_cidrs == ["10.0.0.0/8"]
        assert model.rootfs_snapshot_sources is not None
        assert len(model.rootfs_snapshot_sources) == 1
        assert model.rootfs_snapshot_sources[0].snapshot_name == "snap-1"

        # Dump back to camelCase and re-parse
        dumped = model.model_dump(by_alias=True, mode="json")
        reparsed = SandboxData.model_validate(dumped)

        assert reparsed.id == model.id
        assert reparsed.status == model.status
        assert reparsed.timeout_seconds == model.timeout_seconds
        assert reparsed.network is not None
        assert reparsed.network.allow_cluster_dns == model.network.allow_cluster_dns
        assert reparsed.network.allowed_egress_cidrs == model.network.allowed_egress_cidrs

    def test_create_sandbox_payload_round_trip(self) -> None:
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(
                containers=[Container(
                    image="node:20",
                    command=["node", "app.js"],
                    env={"NODE_ENV": "production"},
                    resources=ResourceRequirements(
                        limits=ResourceList(cpu="2", memory="4Gi"),
                    ),
                )]
            ),
            timeout_seconds=1800,
            startup_timeout_seconds=60,
            network=NetworkSpec(allow_internet_egress=True, nameservers=["1.1.1.1"]),
        )
        dumped = payload.model_dump(by_alias=True, mode="json", exclude_none=True)
        reparsed = CreateSandboxPayload.model_validate(dumped)

        assert reparsed.pod_template.containers[0].image == "node:20"
        assert reparsed.pod_template.containers[0].env == {"NODE_ENV": "production"}
        assert reparsed.timeout_seconds == 1800
        assert reparsed.network is not None
        assert reparsed.network.nameservers == ["1.1.1.1"]

    def test_network_spec_round_trip_preserves_manual_aliases(self) -> None:
        net = NetworkSpec(
            allow_cluster_dns=True,
            allowed_egress_cidrs=["172.16.0.0/12"],
        )
        dumped = net.model_dump(by_alias=True, mode="json", exclude_none=True)
        assert "allowClusterDNS" in dumped
        assert "allowedEgressCIDRs" in dumped

        reparsed = NetworkSpec.model_validate(dumped)
        assert reparsed.allow_cluster_dns is True
        assert reparsed.allowed_egress_cidrs == ["172.16.0.0/12"]

    def test_list_sandboxes_response_round_trip(self) -> None:
        data = {
            "sandboxes": [
                {
                    "id": "sb-1",
                    "status": "running",
                    "creationTimestamp": "2026-01-01T00:00:00Z",
                },
                {
                    "id": "sb-2",
                    "status": "stopped",
                    "creationTimestamp": "2026-01-02T00:00:00Z",
                },
            ]
        }
        model = ListSandboxesResponse.model_validate(data)
        assert model.sandboxes is not None
        assert len(model.sandboxes) == 2

        dumped = model.model_dump(by_alias=True, mode="json")
        reparsed = ListSandboxesResponse.model_validate(dumped)
        assert reparsed.sandboxes is not None
        assert len(reparsed.sandboxes) == 2
        assert reparsed.sandboxes[0].id == "sb-1"
