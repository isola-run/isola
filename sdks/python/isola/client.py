"""Low-level HTTP client for the Isola Gateway API."""

from __future__ import annotations

import base64
import os
import time
from pathlib import Path
from typing import Any, BinaryIO

import httpx

from .exceptions import (
    APIError,
    ConnectionError_,
    FileOperationError,
    TimeoutError_,
)
from .types import (
    ErrorDetail,
    ExecResult,
    FileDownloadResult,
    FileUploadResult,
    HealthStatus,
    LargeFileDownloadResult,
    Sandbox,
    SandboxConfig,
    SandboxList,
    SandboxState,
    UploadUrlResult,
)

DEFAULT_BASE_URL = "http://localhost:8080"
DEFAULT_TIMEOUT = 30.0
LARGE_FILE_THRESHOLD = 5 * 1024 * 1024  # 5MB


class IsolaClient:
    """Low-level HTTP client for the Isola Gateway API.

    This client provides direct access to all isola-gw endpoints.
    For a higher-level API, use the Sandbox builder instead.

    Example:
        client = IsolaClient(api_key="iso_sk_...")
        sandbox = client.create_sandbox(SandboxConfig(name="my-sandbox"))
        result = client.execute_command(sandbox.id, "echo hello")
        print(result.stdout)
        client.terminate_sandbox(sandbox.id)
    """

    def __init__(
        self,
        api_key: str | None = None,
        base_url: str | None = None,
        timeout: float = DEFAULT_TIMEOUT,
    ):
        """Initialize the Isola client.

        Args:
            api_key: API key for authentication. If not provided, reads from
                ISOLA_API_KEY environment variable.
            base_url: Base URL of the Isola Gateway. Defaults to localhost:8080
                or ISOLA_BASE_URL environment variable.
            timeout: Default request timeout in seconds.
        """
        self.api_key = api_key or os.environ.get("ISOLA_API_KEY", "")
        self.base_url = (
            base_url or os.environ.get("ISOLA_BASE_URL") or DEFAULT_BASE_URL
        ).rstrip("/")
        self.timeout = timeout
        self._client = httpx.Client(
            base_url=self.base_url,
            timeout=timeout,
            headers=self._default_headers(),
        )

    def _default_headers(self) -> dict[str, str]:
        headers = {"User-Agent": "isola-python-sdk/1.0.0"}
        if self.api_key:
            headers["X-API-Key"] = self.api_key
        return headers

    def close(self) -> None:
        """Close the HTTP client."""
        self._client.close()

    def __enter__(self) -> IsolaClient:
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def _request(
        self,
        method: str,
        path: str,
        *,
        json: dict[str, Any] | None = None,
        params: dict[str, Any] | None = None,
        data: dict[str, Any] | None = None,
        files: dict[str, Any] | None = None,
        timeout: float | None = None,
    ) -> httpx.Response:
        """Make an HTTP request to the API."""
        try:
            response = self._client.request(
                method,
                path,
                json=json,
                params=params,
                data=data,
                files=files,
                timeout=timeout or self.timeout,
            )
        except httpx.ConnectError as e:
            raise ConnectionError_(f"Failed to connect to {self.base_url}: {e}") from e
        except httpx.TimeoutException as e:
            raise TimeoutError_(f"Request timed out: {e}") from e

        if response.status_code >= 400:
            try:
                error_data = response.json()
                error_detail = ErrorDetail.from_dict(error_data)
            except Exception:
                error_detail = ErrorDetail(
                    error="Unknown",
                    message=response.text or f"HTTP {response.status_code}",
                )
            raise APIError.from_response(response.status_code, error_detail)

        return response

    # Health endpoints

    def health(self) -> HealthStatus:
        """Check the health of the Isola Gateway.

        Returns:
            HealthStatus with status, timestamp, version, and component health.
        """
        response = self._request("GET", "/health")
        return HealthStatus.from_dict(response.json())

    def ready(self) -> bool:
        """Check if the Isola Gateway is ready to accept requests.

        Returns:
            True if ready, False otherwise.
        """
        try:
            response = self._request("GET", "/ready")
            return response.json().get("status") == "ready"
        except APIError:
            return False

    # Sandbox CRUD

    def create_sandbox(self, config: SandboxConfig) -> Sandbox:
        """Create a new sandbox.

        Args:
            config: Sandbox configuration.

        Returns:
            The created Sandbox instance.
        """
        response = self._request("POST", "/api/v1/sandboxes", json=config.to_dict())
        return Sandbox.from_dict(response.json())

    def get_sandbox(self, sandbox_id: str) -> Sandbox:
        """Get a sandbox by ID.

        Args:
            sandbox_id: UUID of the sandbox.

        Returns:
            The Sandbox instance.

        Raises:
            NotFoundError: If sandbox doesn't exist.
        """
        response = self._request("GET", f"/api/v1/sandboxes/{sandbox_id}")
        return Sandbox.from_dict(response.json())

    def list_sandboxes(
        self,
        state: SandboxState | None = None,
        limit: int = 20,
        offset: int = 0,
    ) -> SandboxList:
        """List sandboxes with optional filtering.

        Args:
            state: Filter by sandbox state.
            limit: Maximum number of results (1-100).
            offset: Number of results to skip.

        Returns:
            Paginated list of sandboxes.
        """
        params: dict[str, Any] = {"limit": limit, "offset": offset}
        if state:
            params["state"] = state.value
        response = self._request("GET", "/api/v1/sandboxes", params=params)
        return SandboxList.from_dict(response.json())

    def terminate_sandbox(self, sandbox_id: str, force: bool = False) -> None:
        """Terminate and delete a sandbox.

        Args:
            sandbox_id: UUID of the sandbox.
            force: Force immediate termination without graceful shutdown.

        Raises:
            NotFoundError: If sandbox doesn't exist.
        """
        params = {"force": str(force).lower()} if force else None
        self._request("DELETE", f"/api/v1/sandboxes/{sandbox_id}", params=params)

    def wait_for_sandbox(
        self,
        sandbox_id: str,
        target_state: SandboxState = SandboxState.RUNNING,
        timeout: float = 120.0,
        poll_interval: float = 1.0,
    ) -> Sandbox:
        """Wait for a sandbox to reach a target state.

        Args:
            sandbox_id: UUID of the sandbox.
            target_state: State to wait for.
            timeout: Maximum time to wait in seconds.
            poll_interval: Time between status checks.

        Returns:
            The Sandbox in the target state.

        Raises:
            TimeoutError_: If timeout is reached.
            SandboxError: If sandbox enters error state.
        """
        from .exceptions import SandboxError

        start = time.monotonic()
        while time.monotonic() - start < timeout:
            sandbox = self.get_sandbox(sandbox_id)
            if sandbox.state == target_state:
                return sandbox
            if sandbox.state == SandboxState.ERROR:
                raise SandboxError(
                    f"Sandbox entered error state: {sandbox.error_reason}",
            sandbox_id=sandbox_id,
        )
            time.sleep(poll_interval)

        raise TimeoutError_(
            f"Sandbox {sandbox_id} did not reach {target_state.value} "
            f"within {timeout}s (current: {sandbox.state.value})"
        )

    # Command execution

    def execute_command(
        self,
        sandbox_id: str,
        command: str,
        timeout: float | None = None,
    ) -> ExecResult:
        """Execute a shell command in a sandbox.

        Args:
            sandbox_id: UUID of the sandbox.
            command: Shell command to execute.
            timeout: Command timeout in seconds.

        Returns:
            ExecResult with stdout, stderr, and exit code.

        Raises:
            NotFoundError: If sandbox doesn't exist.
            ConflictError: If sandbox is not running.
        """
        response = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/execute",
            json={"command": command},
            timeout=timeout or 300.0,  # Commands can take longer
        )
        return ExecResult.from_dict(response.json())

    # File operations

    def upload_file(
        self,
        sandbox_id: str,
        local_path: str | Path,
        remote_path: str,
    ) -> FileUploadResult:
        """Upload a file to a sandbox.

        For files larger than 5MB, automatically uses the presigned URL flow.

        Args:
            sandbox_id: UUID of the sandbox.
            local_path: Path to the local file.
            remote_path: Destination path in the sandbox.

        Returns:
            FileUploadResult with success status and file info.

        Raises:
            FileOperationError: If upload fails.
            NotFoundError: If sandbox doesn't exist.
            ConflictError: If sandbox is not running.
        """
        local_path = Path(local_path)
        if not local_path.exists():
            raise FileOperationError(f"File not found: {local_path}", path=str(local_path))

        file_size = local_path.stat().st_size

        if file_size > LARGE_FILE_THRESHOLD:
            return self._upload_large_file(sandbox_id, local_path, remote_path)

        with open(local_path, "rb") as f:
            return self.upload_file_content(sandbox_id, f, remote_path, local_path.name)

    def upload_file_content(
        self,
        sandbox_id: str,
        file: BinaryIO,
        remote_path: str,
        filename: str | None = None,
    ) -> FileUploadResult:
        """Upload file content to a sandbox.

        Args:
            sandbox_id: UUID of the sandbox.
            file: File-like object with content to upload.
            remote_path: Destination path in the sandbox.
            filename: Optional filename for the upload.

        Returns:
            FileUploadResult with success status and file info.
        """
        files = {"file": (filename or "upload", file)}
        data = {"path": remote_path}
        response = self._request(
            "POST",
                f"/api/v1/sandboxes/{sandbox_id}/files",
                data=data,
            files=files,
        )
        return FileUploadResult.from_dict(response.json())

    def upload_bytes(
        self,
        sandbox_id: str,
        content: bytes,
        remote_path: str,
    ) -> FileUploadResult:
        """Upload bytes content to a sandbox.

        Args:
            sandbox_id: UUID of the sandbox.
            content: Bytes to upload.
            remote_path: Destination path in the sandbox.

        Returns:
            FileUploadResult with success status and file info.
        """
        import io

        return self.upload_file_content(
            sandbox_id,
            io.BytesIO(content),
            remote_path,
            Path(remote_path).name,
        )

    def _upload_large_file(
        self,
        sandbox_id: str,
        local_path: Path,
        remote_path: str,
    ) -> FileUploadResult:
        """Upload a large file using presigned URL flow."""
        # Get presigned URL
        url_result = self._get_upload_url(
            sandbox_id,
            remote_path,
            local_path.name,
        )

        # Upload to presigned URL
        with open(local_path, "rb") as f:
            upload_response = httpx.put(
                url_result.upload_url,
                content=f,
                timeout=300.0,
            )
            if upload_response.status_code >= 400:
                raise FileOperationError(
                    f"Failed to upload to presigned URL: {upload_response.status_code}",
                    path=str(local_path),
                )

        # Confirm upload
        return self._confirm_upload(
            sandbox_id,
            url_result.upload_id,
            local_path.name,
            remote_path,
        )

    def _get_upload_url(
        self,
        sandbox_id: str,
        remote_path: str,
        filename: str,
        content_type: str | None = None,
    ) -> UploadUrlResult:
        """Generate a presigned upload URL."""
        json_data: dict[str, Any] = {"path": remote_path, "filename": filename}
        if content_type:
            json_data["content_type"] = content_type
        response = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/upload-url",
            json=json_data,
        )
        return UploadUrlResult.from_dict(response.json())

    def _confirm_upload(
        self,
        sandbox_id: str,
        upload_id: str,
        filename: str,
        remote_path: str,
    ) -> FileUploadResult:
        """Confirm a large file upload."""
        response = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/confirm",
            json={
                "upload_id": upload_id,
                "filename": filename,
                "path": remote_path,
            },
        )
        return FileUploadResult.from_dict(response.json())

    def download_file(
        self,
        sandbox_id: str,
        remote_path: str,
        local_path: str | Path | None = None,
    ) -> FileDownloadResult:
        """Download a file from a sandbox.

        For files larger than 5MB, automatically uses the presigned URL flow.

        Args:
            sandbox_id: UUID of the sandbox.
            remote_path: Path to the file in the sandbox.
            local_path: Optional local path to save the file.

        Returns:
            FileDownloadResult with path, size, and content.

        Raises:
            NotFoundError: If sandbox or file doesn't exist.
            ConflictError: If sandbox is not running.
            FileTooLargeError: If file exceeds direct download limit.
        """
        response = self._request(
            "GET",
            f"/api/v1/sandboxes/{sandbox_id}/files",
            params={"path": remote_path},
        )

        # Check content type to determine response format
        content_type = response.headers.get("content-type", "")

        if "application/json" in content_type:
            # Large file response with download_id
            data = response.json()
            if "download_id" in data:
                return self._download_large_file(sandbox_id, data)
            # Shouldn't happen but handle gracefully
            return FileDownloadResult.from_dict(data)

        # Small file: raw binary response
        result = FileDownloadResult(
            path=remote_path,
            size=len(response.content),
            content=response.content,
        )

        if local_path:
            local_path = Path(local_path)
            local_path.parent.mkdir(parents=True, exist_ok=True)
            local_path.write_bytes(result.content)

        return result

    def _download_large_file(
        self,
        sandbox_id: str,
        initial_response: dict[str, Any],
    ) -> FileDownloadResult:
        """Handle large file download with polling."""
        download_id = initial_response["download_id"]
        remote_path = initial_response.get("path", "")

        # Poll until ready
        for _ in range(120):  # Max 2 minutes
            status = self._get_download_status(sandbox_id, download_id)
            if status.ready and status.download_url:
                # Download from presigned URL
                download_response = httpx.get(status.download_url, timeout=300.0)
                if download_response.status_code >= 400:
                    raise FileOperationError(
                        f"Failed to download from presigned URL: {download_response.status_code}",
                        path=remote_path,
                    )
                return FileDownloadResult(
                    path=status.path or remote_path,
                    size=len(download_response.content),
                    content=download_response.content,
                )
            time.sleep(1)

        raise TimeoutError_(f"Download {download_id} timed out waiting for file to be ready")

    def _get_download_status(
        self,
        sandbox_id: str,
        download_id: str,
    ) -> LargeFileDownloadResult:
        """Get the status of a large file download."""
        response = self._request(
            "GET",
            f"/api/v1/sandboxes/{sandbox_id}/downloads/{download_id}",
        )
        return LargeFileDownloadResult.from_dict(response.json())

    def download_bytes(
        self,
        sandbox_id: str,
        remote_path: str,
    ) -> bytes:
        """Download file content as bytes.

        Args:
            sandbox_id: UUID of the sandbox.
            remote_path: Path to the file in the sandbox.

        Returns:
            File content as bytes.
        """
        result = self.download_file(sandbox_id, remote_path)
        return result.content

    def download_text(
        self,
        sandbox_id: str,
        remote_path: str,
        encoding: str = "utf-8",
    ) -> str:
        """Download file content as text.

        Args:
            sandbox_id: UUID of the sandbox.
            remote_path: Path to the file in the sandbox.
            encoding: Text encoding to use.

        Returns:
            File content as string.
        """
        content = self.download_bytes(sandbox_id, remote_path)
        return content.decode(encoding)


class AsyncIsolaClient:
    """Async HTTP client for the Isola Gateway API.

    Same interface as IsolaClient but with async methods.
    """

    def __init__(
        self,
        api_key: str | None = None,
        base_url: str | None = None,
        timeout: float = DEFAULT_TIMEOUT,
    ):
        self.api_key = api_key or os.environ.get("ISOLA_API_KEY", "")
        self.base_url = (
            base_url or os.environ.get("ISOLA_BASE_URL") or DEFAULT_BASE_URL
        ).rstrip("/")
        self.timeout = timeout
        self._client = httpx.AsyncClient(
            base_url=self.base_url,
            timeout=timeout,
            headers=self._default_headers(),
        )

    def _default_headers(self) -> dict[str, str]:
        headers = {"User-Agent": "isola-python-sdk/1.0.0"}
        if self.api_key:
            headers["X-API-Key"] = self.api_key
        return headers

    async def close(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> AsyncIsolaClient:
        return self

    async def __aexit__(self, *args: Any) -> None:
        await self.close()

    async def _request(
        self,
        method: str,
        path: str,
        *,
        json: dict[str, Any] | None = None,
        params: dict[str, Any] | None = None,
        data: dict[str, Any] | None = None,
        files: dict[str, Any] | None = None,
        timeout: float | None = None,
    ) -> httpx.Response:
        try:
            response = await self._client.request(
                method,
                path,
                json=json,
                params=params,
                data=data,
                files=files,
                timeout=timeout or self.timeout,
            )
        except httpx.ConnectError as e:
            raise ConnectionError_(f"Failed to connect to {self.base_url}: {e}") from e
        except httpx.TimeoutException as e:
            raise TimeoutError_(f"Request timed out: {e}") from e

        if response.status_code >= 400:
            try:
                error_data = response.json()
                error_detail = ErrorDetail.from_dict(error_data)
            except Exception:
                error_detail = ErrorDetail(
                    error="Unknown",
                    message=response.text or f"HTTP {response.status_code}",
                )
            raise APIError.from_response(response.status_code, error_detail)

        return response

    async def health(self) -> HealthStatus:
        response = await self._request("GET", "/health")
        return HealthStatus.from_dict(response.json())

    async def ready(self) -> bool:
        try:
            response = await self._request("GET", "/ready")
            return response.json().get("status") == "ready"
        except APIError:
            return False

    async def create_sandbox(self, config: SandboxConfig) -> Sandbox:
        response = await self._request("POST", "/api/v1/sandboxes", json=config.to_dict())
        return Sandbox.from_dict(response.json())

    async def get_sandbox(self, sandbox_id: str) -> Sandbox:
        response = await self._request("GET", f"/api/v1/sandboxes/{sandbox_id}")
        return Sandbox.from_dict(response.json())

    async def list_sandboxes(
        self,
        state: SandboxState | None = None,
        limit: int = 20,
        offset: int = 0,
    ) -> SandboxList:
        params: dict[str, Any] = {"limit": limit, "offset": offset}
        if state:
            params["state"] = state.value
        response = await self._request("GET", "/api/v1/sandboxes", params=params)
        return SandboxList.from_dict(response.json())

    async def terminate_sandbox(self, sandbox_id: str, force: bool = False) -> None:
        params = {"force": str(force).lower()} if force else None
        await self._request("DELETE", f"/api/v1/sandboxes/{sandbox_id}", params=params)

    async def wait_for_sandbox(
        self,
        sandbox_id: str,
        target_state: SandboxState = SandboxState.RUNNING,
        timeout: float = 120.0,
        poll_interval: float = 1.0,
    ) -> Sandbox:
        import asyncio

        from .exceptions import SandboxError

        start = time.monotonic()
        while time.monotonic() - start < timeout:
            sandbox = await self.get_sandbox(sandbox_id)
            if sandbox.state == target_state:
                return sandbox
            if sandbox.state == SandboxState.ERROR:
                raise SandboxError(
                    f"Sandbox entered error state: {sandbox.error_reason}",
                    sandbox_id=sandbox_id,
                )
            await asyncio.sleep(poll_interval)

        raise TimeoutError_(
            f"Sandbox {sandbox_id} did not reach {target_state.value} "
            f"within {timeout}s (current: {sandbox.state.value})"
        )

    async def execute_command(
        self,
        sandbox_id: str,
        command: str,
        timeout: float | None = None,
    ) -> ExecResult:
        response = await self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/execute",
            json={"command": command},
            timeout=timeout or 300.0,
        )
        return ExecResult.from_dict(response.json())

    async def upload_file(
        self,
        sandbox_id: str,
        local_path: str | Path,
        remote_path: str,
    ) -> FileUploadResult:
        local_path = Path(local_path)
        if not local_path.exists():
            raise FileOperationError(f"File not found: {local_path}", path=str(local_path))

        file_size = local_path.stat().st_size

        if file_size > LARGE_FILE_THRESHOLD:
            return await self._upload_large_file(sandbox_id, local_path, remote_path)

        with open(local_path, "rb") as f:
            return await self.upload_file_content(sandbox_id, f, remote_path, local_path.name)

    async def upload_file_content(
        self,
        sandbox_id: str,
        file: BinaryIO,
        remote_path: str,
        filename: str | None = None,
    ) -> FileUploadResult:
        files = {"file": (filename or "upload", file)}
        data = {"path": remote_path}
        response = await self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files",
            data=data,
            files=files,
        )
        return FileUploadResult.from_dict(response.json())

    async def upload_bytes(
        self,
        sandbox_id: str,
        content: bytes,
        remote_path: str,
    ) -> FileUploadResult:
        import io

        return await self.upload_file_content(
            sandbox_id,
            io.BytesIO(content),
            remote_path,
            Path(remote_path).name,
        )

    async def _upload_large_file(
        self,
        sandbox_id: str,
        local_path: Path,
        remote_path: str,
    ) -> FileUploadResult:
        url_result = await self._get_upload_url(
            sandbox_id,
            remote_path,
            local_path.name,
        )

        async with httpx.AsyncClient() as http:
            with open(local_path, "rb") as f:
                upload_response = await http.put(
                    url_result.upload_url,
                    content=f.read(),
                    timeout=300.0,
                )
                if upload_response.status_code >= 400:
                    raise FileOperationError(
                        f"Failed to upload to presigned URL: {upload_response.status_code}",
                        path=str(local_path),
                    )

        return await self._confirm_upload(
            sandbox_id,
            url_result.upload_id,
            local_path.name,
            remote_path,
        )

    async def _get_upload_url(
        self,
        sandbox_id: str,
        remote_path: str,
        filename: str,
        content_type: str | None = None,
    ) -> UploadUrlResult:
        json_data: dict[str, Any] = {"path": remote_path, "filename": filename}
        if content_type:
            json_data["content_type"] = content_type
        response = await self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/upload-url",
            json=json_data,
        )
        return UploadUrlResult.from_dict(response.json())

    async def _confirm_upload(
        self,
        sandbox_id: str,
        upload_id: str,
        filename: str,
        remote_path: str,
    ) -> FileUploadResult:
        response = await self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/confirm",
            json={
                "upload_id": upload_id,
                "filename": filename,
                "path": remote_path,
            },
        )
        return FileUploadResult.from_dict(response.json())

    async def download_file(
        self,
        sandbox_id: str,
        remote_path: str,
        local_path: str | Path | None = None,
    ) -> FileDownloadResult:
        response = await self._request(
            "GET",
            f"/api/v1/sandboxes/{sandbox_id}/files",
            params={"path": remote_path},
        )

        # Check content type to determine response format
        content_type = response.headers.get("content-type", "")

        if "application/json" in content_type:
            data = response.json()
            if "download_id" in data:
                return await self._download_large_file(sandbox_id, data)
            return FileDownloadResult.from_dict(data)

        # Small file: raw binary response
        result = FileDownloadResult(
            path=remote_path,
            size=len(response.content),
            content=response.content,
        )

        if local_path:
            local_path = Path(local_path)
            local_path.parent.mkdir(parents=True, exist_ok=True)
            local_path.write_bytes(result.content)

        return result

    async def _download_large_file(
        self,
        sandbox_id: str,
        initial_response: dict[str, Any],
    ) -> FileDownloadResult:
        import asyncio

        download_id = initial_response["download_id"]
        remote_path = initial_response.get("path", "")

        for _ in range(120):
            status = await self._get_download_status(sandbox_id, download_id)
            if status.ready and status.download_url:
                async with httpx.AsyncClient() as http:
                    download_response = await http.get(status.download_url, timeout=300.0)
                    if download_response.status_code >= 400:
                        raise FileOperationError(
                            f"Failed to download from presigned URL: {download_response.status_code}",
                            path=remote_path,
                        )
                    return FileDownloadResult(
                        path=status.path or remote_path,
                        size=len(download_response.content),
                        content=download_response.content,
                    )
            await asyncio.sleep(1)

        raise TimeoutError_(f"Download {download_id} timed out waiting for file to be ready")

    async def _get_download_status(
        self,
        sandbox_id: str,
        download_id: str,
    ) -> LargeFileDownloadResult:
        response = await self._request(
            "GET",
            f"/api/v1/sandboxes/{sandbox_id}/downloads/{download_id}",
        )
        return LargeFileDownloadResult.from_dict(response.json())

    async def download_bytes(
        self,
        sandbox_id: str,
        remote_path: str,
    ) -> bytes:
        result = await self.download_file(sandbox_id, remote_path)
        return result.content

    async def download_text(
        self,
        sandbox_id: str,
        remote_path: str,
        encoding: str = "utf-8",
    ) -> str:
        content = await self.download_bytes(sandbox_id, remote_path)
        return content.decode(encoding)
