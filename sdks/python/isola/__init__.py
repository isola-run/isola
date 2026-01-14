"""Isola Python SDK - Remote sandbox execution made simple.

Isola provides isolated compute environments for secure code execution.
This SDK provides a Modal-like API with decorator-based remote execution.

Quick Start:
    >>> import isola
    >>>
    >>> # Create a sandbox configuration
    >>> sandbox = (
    ...     isola.Sandbox("my-sandbox")
    ...     .image("python:3.11-slim")
    ...     .cpu(2)
    ...     .memory(4)
    ... )
    >>>
    >>> # Decorator for remote execution
    >>> @sandbox.function()
    ... def process(data):
    ...     import numpy as np
    ...     return np.mean(data)
    >>>
    >>> # Execute remotely
    >>> result = process.remote([1, 2, 3, 4, 5])

Context Manager Usage:
    >>> with sandbox.run() as session:
    ...     session.upload("data.csv", "/home/user/data.csv")
    ...     result = session.exec("python process.py")
    ...     session.download("/home/user/output.json", "output.json")

Low-Level Client:
    >>> client = isola.IsolaClient(api_key="iso_sk_...")
    >>> sandbox = client.create_sandbox(isola.SandboxConfig(name="test"))
    >>> result = client.execute_command(sandbox.id, "echo hello")
"""

from .client import AsyncIsolaClient, IsolaClient
from .decorators import RemoteFunction
from .exceptions import (
    APIError,
    AuthenticationError,
    AuthorizationError,
    ConflictError,
    ConnectionError_,
    ExecutionError,
    FileOperationError,
    FileTooLargeError,
    GatewayError,
    InternalServerError,
    IsolaError,
    NotFoundError,
    RateLimitError,
    SandboxCreationError,
    SandboxError,
    SandboxNotRunningError,
    SandboxTerminationError,
    SerializationError,
    ServiceUnavailableError,
    TimeoutError_,
    ValidationError,
)
from .sandbox import Sandbox, SandboxSession, create_sandbox
from .types import (
    AttachedVolume,
    ErrorDetail,
    ExecResult,
    FileDownloadResult,
    FileUploadResult,
    HealthStatus,
    LargeFileDownloadResult,
    SandboxConfig,
    SandboxList,
    SandboxState,
    UploadUrlResult,
)
from .types import Sandbox as SandboxInfo

__version__ = "1.0.0"

__all__ = [
    # Version
    "__version__",
    # Main classes
    "Sandbox",
    "SandboxSession",
    "IsolaClient",
    "AsyncIsolaClient",
    "RemoteFunction",
    # Factory functions
    "create_sandbox",
    # Types
    "SandboxConfig",
    "SandboxInfo",
    "SandboxList",
    "SandboxState",
    "ExecResult",
    "FileUploadResult",
    "FileDownloadResult",
    "UploadUrlResult",
    "LargeFileDownloadResult",
    "AttachedVolume",
    "HealthStatus",
    "ErrorDetail",
    # Exceptions
    "IsolaError",
    "APIError",
    "ValidationError",
    "AuthenticationError",
    "AuthorizationError",
    "NotFoundError",
    "ConflictError",
    "FileTooLargeError",
    "RateLimitError",
    "InternalServerError",
    "GatewayError",
    "ServiceUnavailableError",
    "ConnectionError_",
    "TimeoutError_",
    "SandboxError",
    "SandboxNotRunningError",
    "SandboxCreationError",
    "SandboxTerminationError",
    "ExecutionError",
    "SerializationError",
    "FileOperationError",
]


def _configure_logging() -> None:
    """Configure default logging for the SDK."""
    import logging

    logger = logging.getLogger("isola")
    if not logger.handlers:
        handler = logging.StreamHandler()
        handler.setFormatter(
            logging.Formatter("%(asctime)s - %(name)s - %(levelname)s - %(message)s")
        )
        logger.addHandler(handler)
        logger.setLevel(logging.WARNING)


_configure_logging()
