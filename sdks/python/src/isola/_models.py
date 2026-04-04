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

from dataclasses import dataclass
from datetime import datetime
from enum import Enum

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel


@dataclass(frozen=True)
class CommandResult:
    """Result of a completed command execution."""

    id: str
    stdout: str
    stderr: str
    exit_code: int


class IsolaModel(BaseModel):
    model_config = ConfigDict(
        alias_generator=to_camel,
        validate_by_name=True,
        validate_by_alias=True,
        extra="ignore",
    )


class SandboxStatus(str, Enum):
    CREATING = "creating"
    RUNNING = "running"
    SHUTTING_DOWN = "shuttingDown"
    FAILED = "failed"
    STOPPED = "stopped"
    UNKNOWN = "unknown"


class NetworkSpec(IsolaModel):
    allow_internet_egress: bool | None = None
    allow_cluster_dns: bool | None = Field(None, alias="allowClusterDNS")
    allow_ipv6_egress: bool | None = Field(None, alias="allowIPv6Egress")
    allowed_egress_cidrs: list[str] | None = Field(None, alias="allowedEgressCIDRs")
    nameservers: list[str] | None = None


class ResourceList(IsolaModel):
    cpu: str | None = None
    memory: str | None = None
    ephemeral_storage: str | None = None


class ResourcesSpec(IsolaModel):
    limits: ResourceList | None = None
    requests: ResourceList | None = None


class ContainerSpec(IsolaModel):
    image: str
    command: list[str] | None = None
    env: dict[str, str] | None = None
    resources: ResourcesSpec | None = None


class ContainerInfo(IsolaModel):
    image: str
    command: list[str] | None = None
    resources: ResourcesSpec | None = None


class RootfsSnapshotSource(IsolaModel):
    snapshot_name: str
    container_name: str | None = None


class RootfsSnapshotStatus(str, Enum):
    PENDING = "pending"
    IN_PROGRESS = "inProgress"
    COMPLETE = "complete"
    FAILED = "failed"


class CreateRootfsSnapshotPayload(IsolaModel):
    sandbox_id: str
    snapshot_name: str
    container_name: str | None = None
    timeout_seconds: int
    ttl_seconds_after_finished: int


class RootfsSnapshotData(IsolaModel):
    id: str
    sandbox_id: str
    snapshot_name: str
    container_name: str | None = None
    timeout_seconds: int | None = None
    ttl_seconds_after_finished: int | None = None
    status: RootfsSnapshotStatus
    creation_timestamp: datetime


class PodTemplate(IsolaModel):
    container: ContainerSpec


class PodTemplateInfo(IsolaModel):
    container: ContainerInfo


class CreateSandboxPayload(IsolaModel):
    pod_template: PodTemplate
    timeout_seconds: int | None = None
    startup_timeout_seconds: int
    network: NetworkSpec | None = None
    rootfs_snapshot_sources: list[RootfsSnapshotSource] | None = None


class SandboxSummary(IsolaModel):
    id: str
    status: SandboxStatus
    creation_timestamp: datetime


class ListSandboxesResponse(IsolaModel):
    sandboxes: list[SandboxSummary] | None = None


class SandboxData(IsolaModel):
    id: str
    pod_template: PodTemplateInfo
    status: SandboxStatus
    creation_timestamp: datetime
    timeout_seconds: int | None = None
    startup_timeout_seconds: int | None = None
    network: NetworkSpec | None = None
    rootfs_snapshot_sources: list[RootfsSnapshotSource] | None = None


class CreateCommandPayload(IsolaModel):
    args: list[str]
    env: dict[str, str] | None = None
    cwd: str | None = None
    timeout_seconds: int | None = None


class CreateCommandResponse(IsolaModel):
    id: str


class CommandStatusResponse(IsolaModel):
    exit_code: int | None = None


class FileWriteResult(IsolaModel):
    absolute_path: str
    bytes_written: int
