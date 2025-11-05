from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Dict, List, Optional

from pydantic import BaseModel, Field


class VolumeState(str, Enum):
    creating = "creating"
    ready = "ready"
    attached = "attached"
    deleting = "deleting"
    error = "error"


class VolumeAttachment(BaseModel):
    sandboxId: str
    sandboxName: str
    mountPath: str


class Volume(BaseModel):
    id: str
    name: str
    state: VolumeState
    size: int
    attachedTo: List[VolumeAttachment] = Field(default_factory=list)
    labels: Dict[str, str] = Field(default_factory=dict)
    createdAt: datetime
    updatedAt: datetime


class CreateVolume(BaseModel):
    name: str
    size: int
    labels: Optional[Dict[str, str]] = None


class VolumeList(BaseModel):
    items: List[Volume]
    total: int
    limit: int
    offset: int

