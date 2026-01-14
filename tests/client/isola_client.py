"""
Simple API client wrapper for isola-gw.
Future: This will be replaced by the official isola-python SDK.
"""
from __future__ import annotations

import logging
import time
from typing import Any

import requests
from requests.adapters import HTTPAdapter
from urllib3.util.retry import Retry

logger = logging.getLogger(__name__)


class IsolaError(Exception):
    """Base exception for Isola API errors."""

    def __init__(
        self,
        message: str,
        status_code: int | None = None,
        response: dict | None = None,
    ):
        super().__init__(message)
        self.status_code = status_code
        self.response = response or {}

    def __str__(self) -> str:
        if self.status_code:
            return f"[{self.status_code}] {super().__str__()}"
        return super().__str__()


class IsolaClient:
    """Simple wrapper for isola-gw REST API."""

    def __init__(
        self,
        base_url: str,
        api_key: str,
        timeout: int = 30,
    ):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

        # Configure session with retries
        self.session = requests.Session()
        retries = Retry(
            total=3,
            backoff_factor=0.5,
            status_forcelist=[502, 503, 504],
        )
        adapter = HTTPAdapter(max_retries=retries)
        self.session.mount("http://", adapter)
        self.session.mount("https://", adapter)

        # Set default headers
        self.session.headers.update(
            {
                "X-API-Key": self.api_key,
                "Content-Type": "application/json",
            }
        )

    def _request(
        self,
        method: str,
        path: str,
        **kwargs: Any,
    ) -> dict | None:
        """Make an API request."""
        url = f"{self.base_url}{path}"
        kwargs.setdefault("timeout", self.timeout)

        logger.debug(f"{method} {url}")
        response = self.session.request(method, url, **kwargs)

        if response.status_code == 204:
            return None

        try:
            data = response.json()
        except ValueError:
            data = {"raw": response.text}

        if not response.ok:
            raise IsolaError(
                message=data.get("message", data.get("error", f"HTTP {response.status_code}")),
                status_code=response.status_code,
                response=data,
            )

        return data

    # =========================================================================
    # Health endpoints
    # =========================================================================

    def health_check(self) -> bool:
        """Check if the API is healthy."""
        try:
            resp = self.session.get(f"{self.base_url}/health", timeout=5)
            return resp.ok
        except Exception:
            return False

    def ready_check(self) -> bool:
        """Check if the API is ready."""
        try:
            resp = self.session.get(f"{self.base_url}/ready", timeout=5)
            return resp.ok
        except Exception:
            return False

    # =========================================================================
    # Sandbox CRUD
    # =========================================================================

    def create_sandbox(
        self,
        name: str,
        auto_start: bool = True,
        image: str | None = None,
        env: dict[str, str] | None = None,
        labels: dict[str, str] | None = None,
    ) -> dict:
        """Create a new sandbox."""
        payload: dict[str, Any] = {
            "name": name,
            "autoStart": auto_start,
        }
        if image:
            payload["image"] = image
        if env:
            payload["env"] = env
        if labels:
            payload["labels"] = labels

        result = self._request("POST", "/api/v1/sandboxes", json=payload)
        assert result is not None
        return result

    def get_sandbox(self, sandbox_id: str) -> dict:
        """Get sandbox by ID."""
        result = self._request("GET", f"/api/v1/sandboxes/{sandbox_id}")
        assert result is not None
        return result

    def list_sandboxes(
        self,
        state: str | None = None,
        limit: int = 20,
        offset: int = 0,
    ) -> dict:
        """List sandboxes with optional filtering."""
        params: dict[str, Any] = {"limit": limit, "offset": offset}
        if state:
            params["state"] = state
        result = self._request("GET", "/api/v1/sandboxes", params=params)
        assert result is not None
        return result

    def terminate_sandbox(self, sandbox_id: str, force: bool = False) -> None:
        """Terminate a sandbox."""
        params = {"force": "true"} if force else {}
        self._request("DELETE", f"/api/v1/sandboxes/{sandbox_id}", params=params)

    # =========================================================================
    # Command execution
    # =========================================================================

    def execute_command(
        self,
        sandbox_id: str,
        command: str,
        timeout: int | None = None,
    ) -> dict:
        """Execute a command in a sandbox."""
        payload: dict[str, Any] = {"command": command}
        if timeout:
            payload["timeout"] = timeout

        result = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/execute",
            json=payload,
        )
        assert result is not None
        return result

    # =========================================================================
    # File operations
    # =========================================================================

    def upload_file(
        self,
        sandbox_id: str,
        path: str,
        content: bytes,
    ) -> dict:
        """Upload a file directly to a sandbox."""
        result = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files",
            json={"path": path, "content": content.decode("utf-8")},
        )
        assert result is not None
        return result

    def upload_file_multipart(
        self,
        sandbox_id: str,
        path: str,
        content: bytes,
        filename: str = "upload.bin",
    ) -> dict:
        """Upload a file using multipart form (streamed to agent)."""
        url = f"{self.base_url}/api/v1/sandboxes/{sandbox_id}/files/upload"
        files = {"file": (filename, content)}
        data = {"path": path}
        # Don't use session's Content-Type header for multipart
        headers = {"X-API-Key": self.api_key}
        response = self.session.post(
            url,
            files=files,
            data=data,
            headers=headers,
            timeout=self.timeout,
        )
        if not response.ok:
            try:
                data = response.json()
            except ValueError:
                data = {"raw": response.text}
            raise IsolaError(
                message=data.get("message", data.get("error", f"HTTP {response.status_code}")),
                status_code=response.status_code,
                response=data,
            )
        return response.json()

    def generate_upload_url(
        self,
        sandbox_id: str,
        path: str,
        filename: str,
        content_type: str | None = None,
    ) -> dict:
        """Generate a presigned upload URL."""
        payload: dict[str, Any] = {"path": path, "filename": filename}
        if content_type:
            payload["content_type"] = content_type

        result = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/upload-url",
            json=payload,
        )
        assert result is not None
        return result

    def confirm_upload(
        self,
        sandbox_id: str,
        upload_id: str,
        filename: str,
        path: str,
    ) -> dict:
        """Confirm a completed upload."""
        result = self._request(
            "POST",
            f"/api/v1/sandboxes/{sandbox_id}/files/confirm",
            json={
                "upload_id": upload_id,
                "filename": filename,
                "path": path,
            },
        )
        assert result is not None
        return result

    # =========================================================================
    # Waiting utilities
    # =========================================================================

    def wait_for_state(
        self,
        sandbox_id: str,
        target_state: str,
        timeout: int = 60,
        poll_interval: float = 2.0,
    ) -> dict:
        """Wait for a sandbox to reach a specific state."""
        deadline = time.time() + timeout
        last_state = None

        while time.time() < deadline:
            sandbox = self.get_sandbox(sandbox_id)
            current_state = sandbox.get("state")

            if current_state != last_state:
                logger.info(f"Sandbox {sandbox_id}: {last_state} -> {current_state}")
                last_state = current_state

            if current_state == target_state:
                return sandbox

            if current_state in ("error", "failed"):
                raise IsolaError(
                    f"Sandbox entered {current_state} state: {sandbox.get('errorReason', 'unknown')}",
                    response=sandbox,
                )

            time.sleep(poll_interval)

        raise TimeoutError(
            f"Sandbox {sandbox_id} did not reach state '{target_state}' "
            f"within {timeout}s (last state: {last_state})"
        )

    def wait_for_ready(
        self,
        sandbox_id: str,
        timeout: int = 60,
    ) -> dict:
        """Wait for sandbox to be running and ready."""
        return self.wait_for_state(sandbox_id, "running", timeout=timeout)
