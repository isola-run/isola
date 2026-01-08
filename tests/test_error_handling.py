"""
Tests for error handling and edge cases.
"""
from __future__ import annotations

import pytest
import requests

from client.isola_client import IsolaClient, IsolaError


class TestAuthenticationErrors:
    """Test authentication error responses."""

    @pytest.mark.smoke
    def test_missing_api_key(self, base_url: str) -> None:
        """Request without API key should return 401."""
        response = requests.get(
            f"{base_url}/api/v1/sandboxes",
            timeout=10,
        )

        assert response.status_code == 401
        data = response.json()
        assert data.get("error") == "Unauthorized"

    def test_request_with_empty_api_key(self, base_url: str) -> None:
        """Request with empty API key should return 401."""
        response = requests.get(
            f"{base_url}/api/v1/sandboxes",
            headers={"X-API-Key": ""},
            timeout=10,
        )

        assert response.status_code == 401


class TestValidationErrors:
    """Test request validation error responses."""

    def test_create_sandbox_missing_name(self, isola_client: IsolaClient) -> None:
        """Creating sandbox without name should fail."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client._request("POST", "/api/v1/sandboxes", json={})

        assert exc_info.value.status_code == 400

class TestNotFoundErrors:
    """Test 404 error responses."""

    @pytest.mark.smoke
    def test_get_nonexistent_sandbox(self, isola_client: IsolaClient) -> None:
        """Get non-existent sandbox should return 404."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.get_sandbox("nonexistent-sandbox-id")

        assert exc_info.value.status_code == 404

    def test_execute_on_nonexistent_sandbox(self, isola_client: IsolaClient) -> None:
        """Execute on non-existent sandbox should return 404."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.execute_command("nonexistent-sandbox-id", "echo test")

        assert exc_info.value.status_code == 404


class TestHealthEndpoints:
    """Test health and readiness endpoints."""

    @pytest.mark.smoke
    def test_health_endpoint(self, base_url: str) -> None:
        """Health endpoint should return 200."""
        response = requests.get(f"{base_url}/health", timeout=10)

        assert response.status_code == 200
        data = response.json()
        assert data.get("status") in ("healthy", "ok", "up")

    @pytest.mark.smoke
    def test_ready_endpoint(self, base_url: str) -> None:
        """Ready endpoint should return 200 when ready."""
        response = requests.get(f"{base_url}/ready", timeout=10)

        assert response.status_code == 200

    def test_health_no_auth_required(self, base_url: str) -> None:
        """Health endpoint should not require authentication."""
        response = requests.get(
            f"{base_url}/health",
            # Explicitly no auth header
            timeout=10,
        )

        assert response.status_code == 200

    def test_ready_no_auth_required(self, base_url: str) -> None:
        """Ready endpoint should not require authentication."""
        response = requests.get(
            f"{base_url}/ready",
            timeout=10,
        )

        assert response.status_code == 200


class TestInvalidRoutes:
    """Test invalid route handling."""

    def test_invalid_api_route(self, base_url: str, api_key: str) -> None:
        """Invalid API route should return 404."""
        response = requests.get(
            f"{base_url}/api/v1/invalid-route",
            headers={"X-API-Key": api_key},
            timeout=10,
        )

        assert response.status_code == 404

    def test_invalid_method(self, base_url: str, api_key: str) -> None:
        """Invalid HTTP method should return 405."""
        response = requests.patch(
            f"{base_url}/api/v1/sandboxes",
            headers={"X-API-Key": api_key},
            timeout=10,
        )

        # Could be 404 or 405 depending on router behavior
        assert response.status_code in (404, 405)
