from __future__ import annotations

from datetime import datetime
from typing import List, Optional

from pydantic import BaseModel, Field


class ApiKey(BaseModel):
    name: str
    description: Optional[str] = None
    createdAt: datetime
    lastUsedAt: Optional[datetime] = None
    expiresAt: Optional[datetime] = None
    scopes: List[str] = Field(
        default_factory=lambda: [
            "sandboxes:read",
            "sandboxes:write",
            "snapshots:read",
            "volumes:read",
        ]
    )


class CreateApiKey(BaseModel):
    name: str
    description: Optional[str] = None
    expiresAt: Optional[datetime] = None
    scopes: Optional[List[str]] = None


class ApiKeyWithSecret(ApiKey):
    secret: str

