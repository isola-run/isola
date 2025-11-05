from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Optional

from pydantic import BaseModel, Field

from .sandbox import SandboxClass


class RunnerState(str, Enum):
    initializing = "initializing"
    ready = "ready"
    busy = "busy"
    maintenance = "maintenance"
    error = "error"
    offline = "offline"


class RunnerCapacity(BaseModel):
    cpu: int
    memory: int
    disk: int
    gpu: int


class RunnerUsage(BaseModel):
    cpu: int
    memory: int
    disk: int
    gpu: int
    sandboxCount: int


class Runner(BaseModel):
    id: str
    domain: str
    state: RunnerState
    region: str
    class_: SandboxClass = Field(alias="class")
    capacity: RunnerCapacity
    usage: RunnerUsage
    availabilityScore: Optional[float] = 100.0
    lastChecked: Optional[datetime] = None
    version: Optional[str] = "1.0.0"
    createdAt: datetime
    updatedAt: datetime

