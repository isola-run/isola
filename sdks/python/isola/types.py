"""Type definitions for the Isola SDK.

This module contains dataclasses for all API request/response models,
matching the isola-gw OpenAPI specification.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any


class SandboxState(str, Enum):
    """Current state of a sandbox."""

    PENDING = "pending"
    STARTING = "starting"
    RUNNING = "running"
    TERMINATING = "terminating"
    STOPPED = "stopped"
    ERROR = "error"
    UNKNOWN = "unknown"


@dataclass
class AttachedVolume:
    """A volume attached to a sandbox."""

    volume_id: str
    mount_path: str

    def to_dict(self) -> dict[str, str]:
        return {"volumeId": self.volume_id, "mountPath": self.mount_path}


@dataclass
class SandboxConfig:
    """Configuration for creating a sandbox.

    Use the Sandbox builder for a fluent API instead of this directly.
    """

    name: str
    image: str | None = None
    region: str | None = None
    cpu: float | None = None
    memory: float | None = None
    disk: float | None = None
    gpu: int | None = None
    env: dict[str, str] = field(default_factory=dict)
    labels: dict[str, str] = field(default_factory=dict)
    volumes: list[AttachedVolume] = field(default_factory=list)
    auto_start: bool = True

    def to_dict(self) -> dict[str, Any]:
        """Convert to API request format."""
        data: dict[str, Any] = {"name": self.name, "autoStart": self.auto_start}
        if self.image:
            data["image"] = self.image
        if self.region:
            data["region"] = self.region
        if self.cpu is not None:
            data["cpu"] = self.cpu
        if self.memory is not None:
            data["memory"] = self.memory
        if self.disk is not None:
            data["disk"] = self.disk
        if self.gpu is not None:
            data["gpu"] = self.gpu
        if self.env:
            data["env"] = self.env
        if self.labels:
            data["labels"] = self.labels
        if self.volumes:
            data["volumes"] = [v.to_dict() for v in self.volumes]
        return data


@dataclass
class Sandbox:
    """A sandbox instance returned from the API."""

    id: str
    name: str
    state: SandboxState
    env: dict[str, str]
    labels: dict[str, str]
    created_at: datetime
    desired_state: SandboxState | None = None
    error_reason: str | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Sandbox:
        """Create from API response."""
        return cls(
            id=data["id"],
            name=data["name"],
            state=SandboxState(data["state"]),
            env=data.get("env", {}),
            labels=data.get("labels", {}),
            created_at=datetime.fromisoformat(data["createdAt"].replace("Z", "+00:00")),
            desired_state=(
                SandboxState(data["desiredState"]) if data.get("desiredState") else None
            ),
            error_reason=data.get("errorReason"),
        )


@dataclass
class SandboxList:
    """Paginated list of sandboxes."""

    items: list[Sandbox]
    total: int
    limit: int
    offset: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> SandboxList:
        return cls(
            items=[Sandbox.from_dict(item) for item in data["items"]],
            total=data["total"],
            limit=data["limit"],
            offset=data["offset"],
        )


@dataclass
class ExecResult:
    """Result of command execution in a sandbox."""

    stdout: str
    stderr: str
    exit_code: int

    @property
    def success(self) -> bool:
        """Whether the command succeeded (exit code 0)."""
        return self.exit_code == 0

    @property
    def output(self) -> str:
        """Combined stdout and stderr output."""
        parts = []
        if self.stdout:
            parts.append(self.stdout)
        if self.stderr:
            parts.append(self.stderr)
        return "\n".join(parts)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ExecResult:
        return cls(
            stdout=data["stdout"],
            stderr=data["stderr"],
            exit_code=data["exitCode"],
        )


@dataclass
class FileUploadResult:
    """Result of a file upload operation."""

    success: bool
    path: str
    size: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> FileUploadResult:
        return cls(
            success=data["success"],
            path=data["path"],
            size=data["size"],
        )


@dataclass
class FileDownloadResult:
    """Result of a file download operation."""

    path: str
    size: int
    content: bytes

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> FileDownloadResult:
        import base64

        return cls(
            path=data["path"],
            size=data["size"],
            content=base64.b64decode(data["content"]),
        )


@dataclass
class UploadUrlResult:
    """Result of generating a presigned upload URL."""

    upload_url: str
    upload_id: str
    expires_in: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> UploadUrlResult:
        return cls(
            upload_url=data["upload_url"],
            upload_id=data["upload_id"],
            expires_in=data["expires_in"],
        )


@dataclass
class LargeFileDownloadResult:
    """Result of initiating a large file download."""

    download_id: str
    ready: bool
    download_url: str | None = None
    expires_in: int | None = None
    path: str | None = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> LargeFileDownloadResult:
        return cls(
            download_id=data["download_id"],
            ready=data["ready"],
            download_url=data.get("download_url"),
            expires_in=data.get("expires_in"),
            path=data.get("path"),
        )


@dataclass
class ErrorDetail:
    """Error response from the API."""

    error: str
    message: str
    details: dict[str, Any] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ErrorDetail:
        return cls(
            error=data.get("error", "Unknown"),
            message=data.get("message", "Unknown error"),
            details=data.get("details", {}),
        )
