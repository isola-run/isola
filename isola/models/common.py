from __future__ import annotations

from datetime import datetime
from typing import Any, Dict, Optional

from pydantic import BaseModel


class HealthStatus(BaseModel):
    status: str
    timestamp: datetime
    components: Dict[str, bool] = {
        "database": True,
        "runners": True,
        "storage": True,
    }


class Error(BaseModel):
    error: str
    message: str
    details: Optional[Dict[str, Any]] = None


class SystemConfig(BaseModel):
    version: str = "1.0.0"
    defaultSnapshot: str = "ubuntu:22.04"
    sshGatewayHost: str = "ssh.isola.local"
    sshGatewayPort: int = 22
    maxSandboxes: int = 100
    maxVolumes: int = 500
    regions: list[str] = ["default"]


class Labels(BaseModel):
    __root__: Dict[str, str]

    def to_dict(self) -> Dict[str, str]:
        return dict(self.__root__)

