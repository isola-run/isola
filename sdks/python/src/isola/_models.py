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
    """Result of a completed command execution.

    Attributes:
        id: Unique identifier of the command.
        stdout: Complete standard output as a string.
        stderr: Complete standard error as a string.
        exit_code: Process exit code. 0 indicates success.
    """

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
    """Lifecycle status of a sandbox.

    Attributes:
        PENDING: Sandbox is being created (container pulling, scheduling).
        RUNNING: Sandbox is ready and accepting commands.
        TERMINATING: Sandbox is shutting down.
        SUCCEEDED: Sandbox exited normally.
        FAILED: Sandbox failed to start or crashed.
    """

    PENDING = "Pending"
    RUNNING = "Running"
    TERMINATING = "Terminating"
    SUCCEEDED = "Succeeded"
    FAILED = "Failed"


class Network(IsolaModel):
    """Network configuration for a sandbox.

    Sandboxes have no network access by default. Use this to enable
    internet access, cluster DNS, or fine-grained egress rules.

    When internet egress or custom CIDRs are enabled without cluster DNS,
    the server automatically configures public nameservers (8.8.8.8, 1.1.1.1)
    so DNS resolution works out of the box. Override this with the
    nameservers field.

    Attributes:
        allow_internet_egress: Allow outbound traffic to the public internet.
        allow_cluster_dns: Allow DNS resolution through the cluster's DNS
            service. When False and allow_internet_egress or allowed_egress_cidrs are specified,
            the sandbox uses public nameservers or the ones you provide in nameservers.
        allow_ipv6_egress: Allow outbound IPv6 traffic.
        allowed_egress_cidrs: List of CIDR blocks the sandbox can reach
            (e.g. ["10.0.0.0/8"]). Use this for fine-grained control
            instead of allowing all internet traffic.
        nameservers: Custom DNS nameservers. Overrides the automatic
            public nameservers.
    """

    allow_internet_egress: bool | None = None
    allow_cluster_dns: bool | None = Field(None, alias="allowClusterDNS")
    allow_ipv6_egress: bool | None = Field(None, alias="allowIPv6Egress")
    allowed_egress_cidrs: list[str] | None = Field(None, alias="allowedEgressCIDRs")
    nameservers: list[str] | None = None


class ResourceList(IsolaModel):
    cpu: str | None = None
    memory: str | None = None
    ephemeral_storage: str | None = None


class ResourceRequirements(IsolaModel):
    limits: ResourceList | None = None
    requests: ResourceList | None = None


class Container(IsolaModel):
    """Container specification for sandbox creation.

    Used with the containers parameter of Sandboxes.create() when
    running multi-container sandboxes. For single-container sandboxes, pass
    image directly to create() instead.

    Attributes:
        name: Container name. Auto-generated if not set.
        image: Container image to run (e.g. "python:3.12").
        rootfs_snapshot_name: Name of a rootfs snapshot to restore into
            this container.
        command: Command and arguments to run in the container.
        env: Environment variables as key-value pairs.
        resources: CPU, memory, and storage limits.
    """

    name: str | None = None
    image: str
    rootfs_snapshot_name: str | None = None
    command: list[str] | None = None
    env: dict[str, str] | None = None
    resources: ResourceRequirements | None = None


class ContainerInfo(IsolaModel):
    """Read-only container information returned by the API.

    Attributes:
        name: Container name.
        image: Container image.
        rootfs_snapshot_name: Rootfs snapshot name, if restoring from one.
        command: Command and arguments.
        resources: Resource limits and requests.
    """

    name: str | None = None
    image: str
    rootfs_snapshot_name: str | None = None
    command: list[str] | None = None
    resources: ResourceRequirements | None = None


class RootfsSnapshotStatus(str, Enum):
    """Lifecycle status of a rootfs snapshot.

    Attributes:
        PENDING: Snapshot request accepted, not yet started.
        RUNNING: Snapshot is being captured.
        SUCCEEDED: Snapshot completed successfully.
        FAILED: Snapshot failed.
    """

    PENDING = "Pending"
    RUNNING = "Running"
    SUCCEEDED = "Succeeded"
    FAILED = "Failed"


class CreateRootfsSnapshotPayload(IsolaModel):
    sandbox_id: str
    snapshot_name: str | None = None
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
    containers: list[Container]


class PodTemplateInfo(IsolaModel):
    containers: list[ContainerInfo]


class SnapshotRootfs(IsolaModel):
    """Termination policy that snapshots the sandbox filesystem on exit.

    Pass this as the termination_policy parameter of
    Sandboxes.create() to automatically capture a rootfs snapshot
    when the sandbox terminates. Restore the snapshot later by passing
    its name as rootfs_snapshot_name.

    Attributes:
        snapshot_name: Name for the snapshot. If not set, the server
            generates one.
        timeout_seconds: Maximum time for the snapshot operation, in
            seconds. Enforced server-side. The server cancels the
            snapshot if it takes longer than this.
    """

    snapshot_name: str | None = None
    timeout_seconds: int | None = None


class TerminationPolicy(IsolaModel):
    """Wire format for terminationPolicy (internal, not exported)."""

    type: str
    snapshot_rootfs: SnapshotRootfs | None = None


class CreateSandboxPayload(IsolaModel):
    pod_template: PodTemplate
    timeout_seconds: int | None = None
    startup_timeout_seconds: int
    network: Network | None = None
    termination_policy: TerminationPolicy | None = None


class SandboxSummary(IsolaModel):
    """Lightweight sandbox summary returned by list operations.

    Attributes:
        id: Unique identifier of the sandbox.
        status: Current lifecycle status.
        creation_timestamp: When the sandbox was created.
    """

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
    network: Network | None = None
    termination_policy: TerminationPolicy | None = None


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
    """Result of a file write operation.

    Attributes:
        absolute_path: Resolved absolute path where the file was written.
        bytes_written: Number of bytes written.
    """

    absolute_path: str
    bytes_written: int
