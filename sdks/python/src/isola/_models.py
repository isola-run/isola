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

from datetime import datetime
from enum import Enum

from pydantic import BaseModel, ConfigDict, Field
from pydantic.alias_generators import to_camel


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


class CommandResult(IsolaModel):
    command_id: str


class CommandStatus(IsolaModel):
    exit_code: int | None = None


class FileWriteResult(IsolaModel):
    absolute_path: str
    bytes_written: int


class FileInfo(IsolaModel):
    name: str
    path: str
    is_dir: bool
    size: int
    mode: str


class FileListResult(IsolaModel):
    entries: list[FileInfo]


class MkdirResult(IsolaModel):
    absolute_path: str


class RenameResult(IsolaModel):
    absolute_path: str
