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
    env: Dict[str, str] = Field(default_factory=dict)
    labels: Dict[str, str] = Field(default_factory=dict)
    errorReason: Optional[str] = None
    createdAt: datetime


class CreateSandbox(BaseModel):
    name: str
    image: Optional[str] = None
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


class FileUploadRequest(BaseModel):
    path: str = Field(..., description="Target path in the sandbox where the file should be written")
    content: bytes = Field(..., description="File content as bytes")


class FileUploadResponse(BaseModel):
    success: bool = Field(..., description="Whether the upload was successful")
    path: str = Field(..., description="Path where the file was written")
    size: int = Field(..., description="Size of the uploaded file in bytes")


class UploadUrlRequest(BaseModel):
    """Request model for generating a presigned upload URL."""
    path: str = Field(..., description="Target path in the sandbox where the file should be written")
    filename: str = Field(..., description="Name of the file being uploaded")
    content_type: Optional[str] = Field(None, description="Content type of the file (e.g., 'application/octet-stream')")


class UploadUrlResponse(BaseModel):
    """Response model for presigned upload URL."""
    upload_url: str = Field(..., description="Presigned URL for uploading the file directly to S3")
    upload_id: str = Field(..., description="Unique identifier for tracking this upload")
    expires_in: int = Field(..., description="URL expiration time in seconds")


class ConfirmUploadRequest(BaseModel):
    """Request model for confirming an upload and triggering agent download."""
    upload_id: str = Field(..., description="Upload ID returned from upload-url endpoint")
    filename: str = Field(..., description="Name of the file that was uploaded (must match upload-url request)")
    path: str = Field(..., description="Target path in the sandbox where the file should be written")


class DownloadRequest(BaseModel):
    """Request model for downloading a file from S3 to the sandbox (used by agent)."""
    download_url: str = Field(..., description="Presigned URL to download the file from")
    path: str = Field(..., description="Target path in the sandbox where the file should be written")
    s3_key: Optional[str] = Field(None, description="S3 key for the object (required if delete_after is True)")
    delete_after: bool = Field(False, description="Whether to delete the file from S3 after download")


class DownloadResponse(BaseModel):
    """Response model for download operation (used by agent)."""
    success: bool = Field(..., description="Whether the download was successful")
    path: str = Field(..., description="Path where the file was written")
    size: int = Field(..., description="Size of the downloaded file in bytes")
    deleted_from_s3: bool = Field(False, description="Whether the file was deleted from S3")