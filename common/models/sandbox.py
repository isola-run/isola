from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Dict, List, Optional

from pydantic import BaseModel, Field


class SandboxState(str, Enum):
    pending = "pending"
    starting = "starting"
    running = "running"
    terminating = "terminating"
    stopped = "stopped"
    error = "error"
    unknown = "unknown"


class SandboxClass(str, Enum):
    small = "small"
    medium = "medium"
    large = "large"
    xlarge = "xlarge"


class AttachedVolume(BaseModel):
    volumeId: str
    mountPath: str


class ExposedPort(BaseModel):
    port: int
    protocol: str


class Sandbox(BaseModel):
    id: str
    name: str
    state: SandboxState
    desiredState: Optional[SandboxState] = None
    class_: SandboxClass = Field(alias="class")
    env: Dict[str, str] = Field(default_factory=dict)
    labels: Dict[str, str] = Field(default_factory=dict)
    errorReason: Optional[str] = None
    createdAt: datetime


class CreateSandbox(BaseModel):
    name: str
    image: Optional[str] = None
    class_: SandboxClass = Field(default=SandboxClass.small, alias="class")
    region: str = "default"
    cpu: Optional[float] = None
    memory: Optional[float] = None
    disk: Optional[float] = None
    gpu: int = 0
    env: Optional[Dict[str, str]] = None
    labels: Optional[Dict[str, str]] = None
    volumes: Optional[List[AttachedVolume]] = None
    autoStart: bool = True


class SandboxList(BaseModel):
    items: List[Sandbox]
    total: int
    limit: int
    offset: int


class SshAccess(BaseModel):
    host: str
    port: int
    username: str
    command: str
    publicKey: Optional[str] = None


class ExecuteCommandRequest(BaseModel):
    command: str


class ExecuteCommandResponse(BaseModel):
    stdout: str
    stderr: str
    exitCode: int