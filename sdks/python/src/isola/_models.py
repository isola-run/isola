from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from enum import Enum

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel


@dataclass(frozen=True)
class CommandResult:
    """Result of a completed command execution."""

    command_id: str
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


class PodTemplate(IsolaModel):
    container: ContainerSpec


class PodTemplateInfo(IsolaModel):
    container: ContainerInfo


class CreateSandboxPayload(IsolaModel):
    pod_template: PodTemplate
    active_deadline_seconds: int | None = None
    network: NetworkSpec | None = None


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
    active_deadline_seconds: int | None = None
    network: NetworkSpec | None = None


class CreateCommandPayload(IsolaModel):
    cmd: str
    args: list[str] | None = None
    env: dict[str, str] | None = None
    cwd: str | None = None
    timeout: int | None = None


class CreateCommandResponse(IsolaModel):
    command_id: str


class CommandStatusResponse(IsolaModel):
    exit_code: int | None = None


class FileWriteResult(IsolaModel):
    absolute_path: str
    bytes_written: int
