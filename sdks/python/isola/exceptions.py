"""Exception hierarchy for the Isola SDK."""

from __future__ import annotations

from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from .types import ErrorDetail


class IsolaError(Exception):
    """Base exception for all Isola SDK errors."""

    def __init__(self, message: str, details: dict[str, Any] | None = None):
        super().__init__(message)
        self.message = message
        self.details = details or {}


class APIError(IsolaError):
    """Error returned from the Isola API."""

    def __init__(
        self,
        message: str,
        status_code: int,
        error_code: str | None = None,
        details: dict[str, Any] | None = None,
    ):
        super().__init__(message, details)
        self.status_code = status_code
        self.error_code = error_code

    @classmethod
    def from_response(cls, status_code: int, error_detail: ErrorDetail) -> APIError:
        """Create from API error response."""
        error_classes: dict[int, type[APIError]] = {
            400: ValidationError,
            401: AuthenticationError,
            403: AuthorizationError,
            404: NotFoundError,
            409: ConflictError,
            413: FileTooLargeError,
            429: RateLimitError,
            500: InternalServerError,
            501: NotImplementedError_,
            502: GatewayError,
            503: ServiceUnavailableError,
        }
        error_cls = error_classes.get(status_code, APIError)
        return error_cls(
            message=error_detail.message,
            status_code=status_code,
            error_code=error_detail.error,
            details=error_detail.details,
        )

    def __str__(self) -> str:
        parts = [f"[{self.status_code}]"]
        if self.error_code:
            parts.append(f"({self.error_code})")
        parts.append(self.message)
        return " ".join(parts)


class ValidationError(APIError):
    """Invalid request parameters or body (400)."""

    pass


class AuthenticationError(APIError):
    """Missing or invalid API key (401)."""

    pass


class AuthorizationError(APIError):
    """Insufficient permissions (403)."""

    pass


class NotFoundError(APIError):
    """Resource not found (404)."""

    pass


class ConflictError(APIError):
    """Resource state conflict, e.g., sandbox not running (409)."""

    pass


class FileTooLargeError(APIError):
    """File exceeds size limit for direct upload/download (413)."""

    pass


class RateLimitError(APIError):
    """Too many requests (429)."""

    pass


class InternalServerError(APIError):
    """Server-side error (500)."""

    pass


class NotImplementedError_(APIError):
    """Feature not available, e.g., storage not configured (501)."""

    pass


class GatewayError(APIError):
    """Failed to connect to sandbox agent (502)."""

    pass


class ServiceUnavailableError(APIError):
    """Service temporarily unavailable (503)."""

    pass


class ConnectionError_(IsolaError):
    """Failed to connect to the Isola API."""

    pass


class TimeoutError_(IsolaError):
    """Request timed out."""

    pass


class SandboxError(IsolaError):
    """Error related to sandbox operations."""

    def __init__(
        self,
        message: str,
        sandbox_id: str | None = None,
        details: dict[str, Any] | None = None,
    ):
        super().__init__(message, details)
        self.sandbox_id = sandbox_id


class SandboxNotRunningError(SandboxError):
    """Sandbox is not in running state."""

    pass


class SandboxCreationError(SandboxError):
    """Failed to create sandbox."""

    pass


class SandboxTerminationError(SandboxError):
    """Failed to terminate sandbox."""

    pass


class ExecutionError(IsolaError):
    """Error during command execution."""

    def __init__(
        self,
        message: str,
        exit_code: int | None = None,
        stdout: str | None = None,
        stderr: str | None = None,
        details: dict[str, Any] | None = None,
    ):
        super().__init__(message, details)
        self.exit_code = exit_code
        self.stdout = stdout
        self.stderr = stderr


class SerializationError(IsolaError):
    """Failed to serialize or deserialize function/data."""

    pass


class FileOperationError(IsolaError):
    """Error during file upload/download."""

    def __init__(
        self,
        message: str,
        path: str | None = None,
        details: dict[str, Any] | None = None,
    ):
        super().__init__(message, details)
        self.path = path
