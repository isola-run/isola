from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel


class IsolaModel(BaseModel):
    model_config = ConfigDict(
        alias_generator=to_camel,
        populate_by_name=True,
        extra="forbid",
    )


class SandboxStatus(str, Enum):
    CREATING = "creating"
    RUNNING = "running"
    SHUTTING_DOWN = "shuttingDown"
    FAILED = "failed"
    STOPPED = "stopped"
    UNKNOWN = "unknown"


class ErrorDetail(IsolaModel):
    location: str | None = None
    message: str | None = None
    value: Any | None = None


class ErrorResponse(IsolaModel):
    detail: str | None = None
    status: int | None = None
    title: str | None = None
    type: str | None = None
    instance: str | None = None
    errors: list[ErrorDetail] | None = None


class NetworkSpec(IsolaModel):
    allow_internet_egress: bool | None = None
    allow_cluster_dns: bool | None = None
    allowed_egress_cidrs: list[str] | None = None
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


class CommandResult(IsolaModel):
    id: str = Field(alias="commandId")


class CommandStatus(IsolaModel):
    exit_code: int | None = None


class FileWriteResult(IsolaModel):
    absolute_path: str
    bytes_written: int
