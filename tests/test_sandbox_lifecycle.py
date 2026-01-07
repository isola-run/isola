"""
Tests for sandbox lifecycle operations: create, get, list, terminate.
"""
from __future__ import annotations

import time

import pytest

from client.isola_client import IsolaClient, IsolaError


class TestSandboxCreate:
    """Test sandbox creation."""

    @pytest.mark.smoke
    def test_create_sandbox_with_autostart(
        self,
        isola_client: IsolaClient,
        unique_name: str,
    ) -> None:
        """Create a sandbox with autoStart=true and verify it reaches running state."""
        sandbox = isola_client.create_sandbox(
            name=unique_name,
            auto_start=True,
        )

        try:
            assert sandbox["id"] is not None
            assert sandbox["name"] == unique_name
            assert sandbox["state"] in ("pending", "running", "creating")

            # Wait for running
            sandbox = isola_client.wait_for_state(sandbox["id"], "running", timeout=90)
            assert sandbox["state"] == "running"
        finally:
            isola_client.terminate_sandbox(sandbox["id"])

    @pytest.mark.smoke
    def test_create_sandbox_stopped(
        self,
        isola_client: IsolaClient,
        unique_name: str,
    ) -> None:
        """Create a sandbox with autoStart=false."""
        sandbox = isola_client.create_sandbox(
            name=unique_name,
            auto_start=False,
        )

        try:
            assert sandbox["id"] is not None
            assert sandbox["name"] == unique_name
            # State should be pending or stopped depending on implementation
            assert sandbox["state"] in ("pending", "stopped", "creating")
        finally:
            isola_client.terminate_sandbox(sandbox["id"], force=True)

    def test_create_sandbox_with_labels(
        self,
        isola_client: IsolaClient,
        unique_name: str,
    ) -> None:
        """Create sandbox with custom labels."""
        sandbox = isola_client.create_sandbox(
            name=unique_name,
            auto_start=False,
            labels={"team": "platform", "env": "test"},
        )

        try:
            assert sandbox["id"] is not None
            # Labels may or may not be returned depending on implementation
        finally:
            isola_client.terminate_sandbox(sandbox["id"], force=True)


class TestSandboxGet:
    """Test getting sandbox details."""

    @pytest.mark.smoke
    def test_get_sandbox(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Get sandbox by ID."""
        fetched = isola_client.get_sandbox(sandbox["id"])

        assert fetched["id"] == sandbox["id"]
        assert fetched["state"] == "running"

    def test_get_nonexistent_sandbox(self, isola_client: IsolaClient) -> None:
        """Getting a non-existent sandbox returns 404."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.get_sandbox("nonexistent-id-12345")

        assert exc_info.value.status_code == 404


class TestSandboxList:
    """Test listing sandboxes."""

    @pytest.mark.smoke
    def test_list_sandboxes(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """List sandboxes includes our test sandbox."""
        result = isola_client.list_sandboxes()

        assert "items" in result or "sandboxes" in result
        items = result.get("items") or result.get("sandboxes", [])

        sandbox_ids = [s["id"] for s in items]
        assert sandbox["id"] in sandbox_ids

    def test_list_sandboxes_with_limit(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """List sandboxes with limit parameter."""
        result = isola_client.list_sandboxes(limit=1)

        items = result.get("items") or result.get("sandboxes", [])
        assert len(items) <= 1


class TestSandboxTerminate:
    """Test sandbox termination."""

    @pytest.mark.smoke
    def test_terminate_sandbox(
        self,
        isola_client: IsolaClient,
        unique_name: str,
    ) -> None:
        """Terminate a sandbox."""
        # Create sandbox
        sandbox = isola_client.create_sandbox(name=unique_name, auto_start=True)
        isola_client.wait_for_state(sandbox["id"], "running", timeout=90)

        # Terminate
        isola_client.terminate_sandbox(sandbox["id"])

        # Verify gone (should 404 after some time)
        time.sleep(2)

        with pytest.raises(IsolaError) as exc_info:
            isola_client.get_sandbox(sandbox["id"])

        # Should be 404 or state should be terminating/terminated
        assert exc_info.value.status_code in (404, 410)

    def test_terminate_nonexistent_sandbox(self, isola_client: IsolaClient) -> None:
        """Terminate non-existent sandbox should return 404."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.terminate_sandbox("nonexistent-123")

        assert exc_info.value.status_code == 404

    def test_force_terminate_sandbox(
        self,
        isola_client: IsolaClient,
        unique_name: str,
    ) -> None:
        """Force terminate a sandbox."""
        sandbox = isola_client.create_sandbox(name=unique_name, auto_start=True)
        isola_client.wait_for_state(sandbox["id"], "running", timeout=90)

        # Force terminate
        isola_client.terminate_sandbox(sandbox["id"], force=True)

        # Should be gone
        time.sleep(2)

        with pytest.raises(IsolaError) as exc_info:
            isola_client.get_sandbox(sandbox["id"])

        assert exc_info.value.status_code in (404, 410)
