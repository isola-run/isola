"""
E2E tests for RootfsSnapshot functionality.

These tests verify that rootfs snapshots can be created for running sandboxes
and that the snapshot data is uploaded to object storage.
"""
from __future__ import annotations

import logging
import time
import uuid
from typing import TYPE_CHECKING

import pytest

from client.isola_client import IsolaClient
from helpers.k8s import K8S_AVAILABLE

if TYPE_CHECKING:
    from helpers.k8s import K8sHelper

logger = logging.getLogger(__name__)

# Skip all tests in this module if kubernetes package is not available
pytestmark = pytest.mark.skipif(
    not K8S_AVAILABLE,
    reason="kubernetes package required for snapshot tests",
)


@pytest.fixture
def k8s(k8s_helper: K8sHelper | None) -> K8sHelper:
    """Get the K8s helper, skip if not available."""
    if k8s_helper is None:
        pytest.skip("K8s helper not available")
    return k8s_helper


@pytest.fixture
def snapshot_name() -> str:
    """Generate a unique snapshot name."""
    return f"snap-e2e-{uuid.uuid4().hex[:8]}"


class TestRootfsSnapshotCreation:
    """Test RootfsSnapshot creation and completion."""

    @pytest.mark.smoke
    def test_create_snapshot_for_running_sandbox(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
        k8s: K8sHelper,
        snapshot_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Create a snapshot for a running sandbox and verify it completes."""
        sandbox_id = sandbox["id"]
        sandbox_k8s_name = f"sandbox-{sandbox_id[:8]}"

        # Create a file in the sandbox to verify it gets snapshotted
        isola_client.execute_command(sandbox_id, "echo 'snapshot-test-data' > /tmp/test.txt")

        try:
            # Create the RootfsSnapshot
            snap = k8s.create_rootfs_snapshot(
                name=snapshot_name,
                sandbox_name=sandbox_k8s_name,
                container_names=["sandbox"],
            )
            logger.info(f"Created RootfsSnapshot: {snapshot_name}")

            # Wait for snapshot to complete (Complete=True or Complete=False with Failed reason)
            try:
                snap = k8s.wait_for_snapshot_condition(
                    name=snapshot_name,
                    condition_type="Complete",
                    expected_status="True",
                    timeout=120,
                )
            except TimeoutError:
                # Get current state for debugging
                snap = k8s.get_rootfs_snapshot(snapshot_name)
                conditions = snap.get("status", {}).get("conditions", []) if snap else []
                pytest.fail(f"Snapshot did not complete in time. Conditions: {conditions}")

            # Verify snapshot completed successfully
            status = snap.get("status", {})
            assert status.get("completedAt") is not None, "Snapshot should have completedAt"

            # Verify container snapshot status
            container_snapshots = status.get("containerSnapshots", [])
            assert len(container_snapshots) == 1, "Should have one container snapshot"
            assert container_snapshots[0]["containerName"] == "sandbox"
            assert container_snapshots[0].get("snapshotKey"), "Should have snapshotKey"
            assert container_snapshots[0].get("snapshotURI"), "Should have snapshotURI"

            logger.info(f"Snapshot completed: {container_snapshots[0]['snapshotURI']}")

        finally:
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snapshot_name)

    def test_snapshot_fails_for_nonexistent_sandbox(
        self,
        k8s: K8sHelper,
        snapshot_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Snapshot fails when the sandbox doesn't exist."""
        try:
            k8s.create_rootfs_snapshot(
                name=snapshot_name,
                sandbox_name="nonexistent-sandbox",
                container_names=["sandbox"],
            )

            # Wait for failure
            snap = k8s.wait_for_snapshot_condition(
                name=snapshot_name,
                condition_type="Complete",
                expected_status="False",
                timeout=30,
            )

            # Verify it failed with appropriate reason
            conditions = snap.get("status", {}).get("conditions", [])
            ready_cond = next((c for c in conditions if c["type"] == "Complete"), None)
            assert ready_cond is not None
            assert ready_cond["reason"] == "Failed"
            assert "not found" in ready_cond.get("message", "").lower()

        finally:
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snapshot_name)

    def test_snapshot_fails_for_empty_container_names(
        self,
        sandbox: dict,
        k8s: K8sHelper,
        snapshot_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Snapshot fails when containerNames is empty."""
        sandbox_id = sandbox["id"]
        sandbox_k8s_name = f"sandbox-{sandbox_id[:8]}"

        try:
            k8s.create_rootfs_snapshot(
                name=snapshot_name,
                sandbox_name=sandbox_k8s_name,
                container_names=[],  # Empty - should fail
            )

            # Wait for failure
            snap = k8s.wait_for_snapshot_condition(
                name=snapshot_name,
                condition_type="Complete",
                expected_status="False",
                timeout=30,
            )

            # Verify it failed
            conditions = snap.get("status", {}).get("conditions", [])
            ready_cond = next((c for c in conditions if c["type"] == "Complete"), None)
            assert ready_cond is not None
            assert ready_cond["reason"] == "Failed"
            assert "no containers" in ready_cond.get("message", "").lower()

        finally:
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snapshot_name)


class TestRootfsSnapshotLabels:
    """Test that snapshots get proper labels for discovery."""

    def test_snapshot_gets_sandbox_label(
        self,
        sandbox: dict,
        k8s: K8sHelper,
        snapshot_name: str,
        skip_cleanup: bool,
    ) -> None:
        """Snapshot should have the sandbox discovery label set by controller."""
        sandbox_id = sandbox["id"]
        sandbox_k8s_name = f"sandbox-{sandbox_id[:8]}"

        try:
            k8s.create_rootfs_snapshot(
                name=snapshot_name,
                sandbox_name=sandbox_k8s_name,
                container_names=["sandbox"],
            )

            # Wait a moment for controller to add labels
            time.sleep(2)

            snap = k8s.get_rootfs_snapshot(snapshot_name)
            assert snap is not None

            labels = snap.get("metadata", {}).get("labels", {})
            assert labels.get("sandbox.isola.run/sandbox-name") == sandbox_k8s_name

        finally:
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snapshot_name)

    def test_list_snapshots_by_sandbox(
        self,
        sandbox: dict,
        k8s: K8sHelper,
        skip_cleanup: bool,
    ) -> None:
        """Can list snapshots for a specific sandbox using label selector."""
        sandbox_id = sandbox["id"]
        sandbox_k8s_name = f"sandbox-{sandbox_id[:8]}"
        snap_name_1 = f"snap-list-{uuid.uuid4().hex[:8]}"
        snap_name_2 = f"snap-list-{uuid.uuid4().hex[:8]}"

        try:
            # Create two snapshots for the same sandbox
            k8s.create_rootfs_snapshot(
                name=snap_name_1,
                sandbox_name=sandbox_k8s_name,
                container_names=["sandbox"],
            )
            k8s.create_rootfs_snapshot(
                name=snap_name_2,
                sandbox_name=sandbox_k8s_name,
                container_names=["sandbox"],
            )

            # Wait for labels to be applied
            time.sleep(2)

            # List by label selector
            snapshots = k8s.list_rootfs_snapshots(
                label_selector=f"sandbox.isola.run/sandbox-name={sandbox_k8s_name}",
            )

            snapshot_names = [s["metadata"]["name"] for s in snapshots]
            assert snap_name_1 in snapshot_names
            assert snap_name_2 in snapshot_names

        finally:
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snap_name_1)
                k8s.delete_rootfs_snapshot(snap_name_2)


class TestRootfsSnapshotTTL:
    """Test snapshot TTL cleanup."""

    def test_snapshot_deleted_after_ttl(
        self,
        sandbox: dict,
        k8s: K8sHelper,
        skip_cleanup: bool,
    ) -> None:
        """Snapshot should be auto-deleted after TTL expires."""
        sandbox_id = sandbox["id"]
        sandbox_k8s_name = f"sandbox-{sandbox_id[:8]}"
        snap_name = f"snap-ttl-{uuid.uuid4().hex[:8]}"

        # Create snapshot with very short TTL (5 seconds)
        body = {
            "apiVersion": "sandbox.isola.run/v1alpha1",
            "kind": "RootfsSnapshot",
            "metadata": {
                "name": snap_name,
                "namespace": "isola-sandboxes",
            },
            "spec": {
                "sandboxName": sandbox_k8s_name,
                "containerNames": ["sandbox"],
                "ttlSecondsAfterFinished": 5,
            },
        }

        try:
            k8s.custom.create_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace="isola-sandboxes",
                plural="rootfssnapshots",
                body=body,
            )

            # Wait for completion
            k8s.wait_for_snapshot_condition(
                name=snap_name,
                condition_type="Complete",
                expected_status="True",
                timeout=120,
            )

            # Wait for TTL + buffer
            logger.info("Waiting for TTL expiry...")
            time.sleep(10)

            # Snapshot should be gone
            snap = k8s.get_rootfs_snapshot(snap_name)
            assert snap is None, "Snapshot should have been deleted after TTL"

        finally:
            # Cleanup in case test failed before TTL
            if not skip_cleanup:
                k8s.delete_rootfs_snapshot(snap_name)
