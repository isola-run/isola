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

from datetime import datetime, timezone

import pytest
from pydantic import ValidationError

from isola._models import (
    CommandResult,
    CommandStatusResponse,
    ContainerInfo,
    ContainerSpec,
    CreateCommandPayload,
    CreateCommandResponse,
    CreateSandboxPayload,
    FileWriteResult,
    ListSandboxesResponse,
    NetworkSpec,
    PodTemplate,
    PodTemplateInfo,
    ResourceList,
    ResourcesSpec,
    RootfsSnapshotSource,
    SandboxData,
    SandboxStatus,
    SandboxSummary,
)


# --- Alias generation (to_camel) ---


class TestAliasCamelCase:
    def test_resource_list_ephemeral_storage(self) -> None:
        rl = ResourceList(ephemeral_storage="10Gi")
        dumped = rl.model_dump(by_alias=True)
        assert "ephemeralStorage" in dumped
        assert dumped["ephemeralStorage"] == "10Gi"

    def test_create_sandbox_payload_aliases(self) -> None:
        payload = CreateSandboxPayload(
            pod_template=PodTemplate(container=ContainerSpec(image="python:3.12")),
            timeout_seconds=600,
            startup_timeout_seconds=30,
        )
        dumped = payload.model_dump(by_alias=True, exclude_none=True)
        assert "podTemplate" in dumped
        assert "timeoutSeconds" in dumped
        assert "startupTimeoutSeconds" in dumped
        # snake_case keys should not appear
        assert "pod_template" not in dumped
        assert "timeout_seconds" not in dumped

    def test_create_command_payload_aliases(self) -> None:
        payload = CreateCommandPayload(args=["ls", "-la"], timeout_seconds=10, cwd="/tmp")
        dumped = payload.model_dump(by_alias=True, exclude_none=True)
        assert "timeoutSeconds" in dumped
        assert dumped["args"] == ["ls", "-la"]
        assert dumped["cwd"] == "/tmp"

    def test_file_write_result_aliases(self) -> None:
        result = FileWriteResult(absolute_path="/tmp/file.txt", bytes_written=42)
        dumped = result.model_dump(by_alias=True)
        assert "absolutePath" in dumped
        assert "bytesWritten" in dumped

    def test_rootfs_snapshot_source_aliases(self) -> None:
        source = RootfsSnapshotSource(snapshot_name="snap-1", container_name="main")
        dumped = source.model_dump(by_alias=True, exclude_none=True)
        assert "snapshotName" in dumped
        assert "containerName" in dumped

    def test_command_status_response_alias(self) -> None:
        resp = CommandStatusResponse(exit_code=0)
        dumped = resp.model_dump(by_alias=True)
        assert "exitCode" in dumped

    def test_create_command_response_alias(self) -> None:
        resp = CreateCommandResponse(command_id="cmd-abc")
        dumped = resp.model_dump(by_alias=True)
        assert "commandId" in dumped
        assert dumped["commandId"] == "cmd-abc"


# --- NetworkSpec manual aliases ---


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
        spec = ContainerSpec(image="ubuntu:22.04")
        assert spec.image == "ubuntu:22.04"

    def test_construct_with_camel_case(self) -> None:
        payload = CreateSandboxPayload.model_validate({
            "podTemplate": {"container": {"image": "node:20"}},
            "timeoutSeconds": 300,
        })
        assert payload.pod_template.container.image == "node:20"
        assert payload.timeout_seconds == 300

    def test_construct_with_snake_case_dict(self) -> None:
        payload = CreateSandboxPayload.model_validate({
            "pod_template": {"container": {"image": "node:20"}},
            "timeout_seconds": 300,
        })
        assert payload.pod_template.container.image == "node:20"
        assert payload.timeout_seconds == 300

    def test_network_spec_by_alias(self) -> None:
        net = NetworkSpec.model_validate({
            "allowClusterDNS": True,
            "allowedEgressCIDRs": ["10.0.0.0/8"],
        })
        assert net.allow_cluster_dns is True
        assert net.allowed_egress_cidrs == ["10.0.0.0/8"]

    def test_network_spec_by_name(self) -> None:
        net = NetworkSpec.model_validate({
            "allow_cluster_dns": False,
            "allowed_egress_cidrs": ["192.168.0.0/16"],
        })
        assert net.allow_cluster_dns is False
        assert net.allowed_egress_cidrs == ["192.168.0.0/16"]


# --- extra="ignore" ---


class TestExtraIgnore:
    def test_unknown_fields_silently_dropped(self) -> None:
        spec = ContainerSpec.model_validate({
            "image": "python:3.12",
            "unknownField": "should be ignored",
            "anotherUnknown": 42,
        })
        assert spec.image == "python:3.12"
        assert not hasattr(spec, "unknownField")
        assert not hasattr(spec, "anotherUnknown")

    def test_sandbox_data_ignores_extra(self) -> None:
        data = SandboxData.model_validate({
            "id": "sb-1",
            "status": "running",
            "creationTimestamp": "2026-01-01T00:00:00Z",
            "podTemplate": {"container": {"image": "alpine"}},
            "futureField": "from newer API version",
        })
        assert data.id == "sb-1"
        assert not hasattr(data, "futureField")

    def test_network_spec_ignores_extra(self) -> None:
        net = NetworkSpec.model_validate({"newPolicy": "allow-all"})
        assert not hasattr(net, "newPolicy")


# --- Required vs optional fields ---


class TestRequiredOptionalFields:
    def test_container_spec_requires_image(self) -> None:
        with pytest.raises(ValidationError) as exc_info:
            ContainerSpec.model_validate({})
        errors = exc_info.value.errors()
        assert any(e["loc"] == ("image",) for e in errors)

    def test_container_spec_optional_fields_default_none(self) -> None:
        spec = ContainerSpec(image="alpine")
        assert spec.command is None
        assert spec.env is None
        assert spec.resources is None

    def test_create_sandbox_payload_requires_pod_template(self) -> None:
        with pytest.raises(ValidationError) as exc_info:
            CreateSandboxPayload.model_validate({})
        errors = exc_info.value.errors()
        field_locs = {e["loc"] for e in errors}
        assert ("pod_template",) in field_locs or ("podTemplate",) in field_locs

    def test_pod_template_requires_container(self) -> None:
        with pytest.raises(ValidationError):
            PodTemplate.model_validate({})

    def test_create_command_payload_requires_args(self) -> None:
        with pytest.raises(ValidationError) as exc_info:
            CreateCommandPayload.model_validate({})
        errors = exc_info.value.errors()
        assert any("args" in str(e["loc"]) for e in errors)

    def test_file_write_result_requires_both_fields(self) -> None:
        with pytest.raises(ValidationError):
            FileWriteResult.model_validate({})

    def test_sandbox_summary_requires_all_fields(self) -> None:
        with pytest.raises(ValidationError):
            SandboxSummary.model_validate({})

    def test_sandbox_data_optional_fields_default_none(self) -> None:
        data = SandboxData.model_validate({
            "id": "sb-1",
            "status": "running",
            "creationTimestamp": "2026-01-01T00:00:00Z",
            "podTemplate": {"container": {"image": "alpine"}},
        })
        assert data.timeout_seconds is None
        assert data.startup_timeout_seconds is None
        assert data.network is None
        assert data.rootfs_snapshot_sources is None

    def test_command_status_response_exit_code_optional(self) -> None:
        resp = CommandStatusResponse.model_validate({})
        assert resp.exit_code is None

    def test_rootfs_snapshot_source_requires_snapshot_name(self) -> None:
        with pytest.raises(ValidationError):
            RootfsSnapshotSource.model_validate({})

    def test_rootfs_snapshot_source_container_name_optional(self) -> None:
        source = RootfsSnapshotSource(snapshot_name="snap-1")
        assert source.container_name is None


# --- SandboxStatus enum ---


class TestSandboxStatus:
    def test_all_values(self) -> None:
        expected = {"creating", "running", "shuttingDown", "failed", "stopped", "unknown"}
        actual = {s.value for s in SandboxStatus}
        assert actual == expected

    def test_enum_from_value(self) -> None:
        assert SandboxStatus("running") is SandboxStatus.RUNNING
        assert SandboxStatus("shuttingDown") is SandboxStatus.SHUTTING_DOWN

    def test_unknown_value_raises(self) -> None:
        with pytest.raises(ValueError):
            SandboxStatus("nonexistent")

    def test_is_string(self) -> None:
        assert isinstance(SandboxStatus.RUNNING, str)
        assert SandboxStatus.RUNNING == "running"

    def test_sandbox_data_parses_status(self) -> None:
        data = SandboxData.model_validate({
            "id": "sb-1",
            "status": "creating",
            "creationTimestamp": "2026-01-01T00:00:00Z",
            "podTemplate": {"container": {"image": "alpine"}},
        })
        assert data.status is SandboxStatus.CREATING


# --- CommandResult dataclass ---


class TestCommandResult:
    def test_construction_and_access(self) -> None:
        result = CommandResult(command_id="cmd-1", stdout="hello", stderr="", exit_code=0)
        assert result.command_id == "cmd-1"
        assert result.stdout == "hello"
        assert result.stderr == ""
        assert result.exit_code == 0

    def test_frozen(self) -> None:
        result = CommandResult(command_id="cmd-1", stdout="", stderr="", exit_code=0)
        with pytest.raises(AttributeError):
            result.exit_code = 1  # type: ignore[misc]

    def test_equality(self) -> None:
        a = CommandResult(command_id="cmd-1", stdout="out", stderr="err", exit_code=0)
        b = CommandResult(command_id="cmd-1", stdout="out", stderr="err", exit_code=0)
        assert a == b

    def test_inequality(self) -> None:
        a = CommandResult(command_id="cmd-1", stdout="out", stderr="err", exit_code=0)
        b = CommandResult(command_id="cmd-2", stdout="out", stderr="err", exit_code=0)
        assert a != b


# --- Round-trip serialization ---


class TestRoundTrip:
    def test_sandbox_data_round_trip(self) -> None:
        camel_json = {
            "id": "sb-42",
            "status": "running",
            "creationTimestamp": "2026-03-15T12:30:00Z",
            "podTemplate": {
                "container": {
                    "image": "python:3.12",
                    "command": ["sleep", "infinity"],
                    "resources": {
                        "limits": {"cpu": "1", "memory": "2Gi", "ephemeralStorage": "5Gi"},
                        "requests": {"cpu": "500m", "memory": "1Gi"},
                    },
                }
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
                container=ContainerSpec(
                    image="node:20",
                    command=["node", "app.js"],
                    env={"NODE_ENV": "production"},
                    resources=ResourcesSpec(
                        limits=ResourceList(cpu="2", memory="4Gi"),
                    ),
                )
            ),
            timeout_seconds=1800,
            network=NetworkSpec(allow_internet_egress=True, nameservers=["1.1.1.1"]),
        )
        dumped = payload.model_dump(by_alias=True, mode="json", exclude_none=True)
        reparsed = CreateSandboxPayload.model_validate(dumped)

        assert reparsed.pod_template.container.image == "node:20"
        assert reparsed.pod_template.container.env == {"NODE_ENV": "production"}
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


# --- Type coercion / datetime parsing ---


class TestTypeCoercion:
    def test_datetime_parsing_iso_format(self) -> None:
        summary = SandboxSummary.model_validate({
            "id": "sb-1",
            "status": "running",
            "creationTimestamp": "2026-03-15T12:30:00Z",
        })
        assert isinstance(summary.creation_timestamp, datetime)
        assert summary.creation_timestamp.year == 2026
        assert summary.creation_timestamp.month == 3
        assert summary.creation_timestamp.day == 15

    def test_datetime_parsing_with_offset(self) -> None:
        summary = SandboxSummary.model_validate({
            "id": "sb-1",
            "status": "running",
            "creationTimestamp": "2026-06-01T08:00:00+05:00",
        })
        assert isinstance(summary.creation_timestamp, datetime)
        assert summary.creation_timestamp.tzinfo is not None

    def test_sandbox_data_datetime(self) -> None:
        data = SandboxData.model_validate({
            "id": "sb-1",
            "status": "running",
            "creationTimestamp": "2026-01-01T00:00:00Z",
            "podTemplate": {"container": {"image": "alpine"}},
        })
        assert isinstance(data.creation_timestamp, datetime)
        assert data.creation_timestamp == datetime(2026, 1, 1, tzinfo=timezone.utc)

    def test_invalid_datetime_raises_validation_error(self) -> None:
        with pytest.raises(ValidationError):
            SandboxSummary.model_validate({
                "id": "sb-1",
                "status": "running",
                "creationTimestamp": "not-a-date",
            })


# --- Nested model construction ---


class TestNestedModels:
    def test_resources_spec_nested(self) -> None:
        spec = ResourcesSpec(
            limits=ResourceList(cpu="1", memory="2Gi", ephemeral_storage="10Gi"),
            requests=ResourceList(cpu="500m", memory="1Gi"),
        )
        assert spec.limits is not None
        assert spec.limits.cpu == "1"
        assert spec.limits.ephemeral_storage == "10Gi"
        assert spec.requests is not None
        assert spec.requests.ephemeral_storage is None

    def test_container_info_vs_container_spec(self) -> None:
        """ContainerInfo has no env field (write-only in requests)."""
        info = ContainerInfo.model_validate({
            "image": "python:3.12",
            "command": ["sleep", "infinity"],
        })
        assert info.image == "python:3.12"
        assert not hasattr(info, "env")

    def test_pod_template_info_nested(self) -> None:
        info = PodTemplateInfo.model_validate({
            "container": {
                "image": "alpine",
                "resources": {
                    "limits": {"cpu": "100m"},
                },
            }
        })
        assert info.container.image == "alpine"
        assert info.container.resources is not None
        assert info.container.resources.limits is not None
        assert info.container.resources.limits.cpu == "100m"
