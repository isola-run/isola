from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import List, Optional

from pydantic import BaseModel


class SnapshotState(str, Enum):
    pending = "pending"
    building = "building"
    active = "active"
    failed = "failed"
    deleting = "deleting"


class Snapshot(BaseModel):
    id: str
    name: str
    state: SnapshotState
    imageName: str
    general: bool = False
    size: Optional[float] = None
    cpu: int = 1
    memory: int = 1
    disk: int = 10
    gpu: int = 0
    entrypoint: Optional[List[str]] = None
    errorReason: Optional[str] = None
    createdAt: datetime
    updatedAt: datetime
    lastUsedAt: Optional[datetime] = None


class CreateSnapshot(BaseModel):
    name: str
    sandboxId: Optional[str] = None
    dockerfile: Optional[str] = None
    imageName: Optional[str] = None
    cpu: Optional[int] = None
    memory: Optional[int] = None
    disk: Optional[int] = None
    gpu: Optional[int] = None
    entrypoint: Optional[List[str]] = None


class SnapshotList(BaseModel):
    items: List[Snapshot]
    total: int
    limit: int
    offset: int

