# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import subprocess
import time

import pytest

from isola import Isola, RootfsSnapshotStatus, Sandbox, SandboxStatus

from conftest import (
    SANDBOXES_NAMESPACE,
    wait_for_visible,
    wait_for_running,
)


@pytest.mark.timeout(120)
class TestRootfsSnapshotSourcesField:
    """Verify the rootfsSnapshotSources field round-trips through the API."""

    def test_create_response_includes_rootfs_snapshot_sources(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        sb = sandbox_factory(rootfs_snapshot_source="nonexistent-snap", max_wait_seconds=0)
        assert sb.rootfs_snapshot_sources is not None
        assert len(sb.rootfs_snapshot_sources) == 1
        assert sb.rootfs_snapshot_sources[0].snapshot_name == "nonexistent-snap"

    def test_get_response_includes_rootfs_snapshot_sources(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        sb = sandbox_factory(rootfs_snapshot_source="nonexistent-snap", max_wait_seconds=0)
        fetched = wait_for_visible(isola_client, sb.sandbox_id)
        assert fetched.rootfs_snapshot_sources is not None
        assert len(fetched.rootfs_snapshot_sources) == 1
        assert fetched.rootfs_snapshot_sources[0].snapshot_name == "nonexistent-snap"

    def test_create_without_snapshot_has_no_sources(
        self, session_sandbox: Sandbox
    ) -> None:
        sources = session_sandbox.rootfs_snapshot_sources
        assert sources is None or sources == []

    def test_sandbox_with_nonexistent_snapshot_retries(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        """With restartPolicyRules, a nonexistent snapshot causes exit 128 retries."""
        sb = sandbox_factory(
            rootfs_snapshot_source="does-not-exist",
            startup_timeout_seconds=120,
            max_wait_seconds=0,
        )

        # Wait for at least one restart (container exits 128, kubelet retries)
        deadline = time.monotonic() + 60
        restarts = 0
        while time.monotonic() < deadline:
            result = subprocess.run(
                [
                    "kubectl", "get", "pod", f"{sb.sandbox_id}-pod",
                    "-n", SANDBOXES_NAMESPACE,
                    "-o", "jsonpath={.status.containerStatuses[0].restartCount}",
                ],
                capture_output=True, text=True,
            )
            if result.returncode == 0 and result.stdout.strip():
                restarts = int(result.stdout.strip())
                if restarts > 0:
                    break
            time.sleep(2)

        assert restarts > 0, "Expected pod to restart on exit code 128"

        # Sandbox should stay in creating (not failed)
        current = isola_client.sandboxes.get(sb.sandbox_id)
        assert current.status == SandboxStatus.CREATING


@pytest.mark.timeout(180)
class TestRootfsRestoreWorkflow:
    """Full end-to-end: create sandbox, write data, snapshot, restore, verify."""

    def test_snapshot_and_restore_via_python_sdk(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        # 1. Create a sandbox and write a marker file
        sb = sandbox_factory()
        running = wait_for_running(isola_client, sb.sandbox_id)
        result = running.commands.run("sh", "-c", "echo 'sdk-restored-data-marker' > /root/sdk-restore-test.txt")
        assert result.exit_code == 0

        # 2. Create a rootfs snapshot through the Python SDK and wait for completion
        snapshot_name = f"e2e-sdk-restore-{sb.sandbox_id}"
        snapshot = isola_client.rootfs_snapshots.create(
            sandbox_id=sb.sandbox_id,
            snapshot_name=snapshot_name,
            max_wait_seconds=120,
        )

        assert snapshot.sandbox_id == sb.sandbox_id
        assert snapshot.snapshot_name == snapshot_name
        assert snapshot.status == RootfsSnapshotStatus.COMPLETE

        fetched = isola_client.rootfs_snapshots.get(snapshot.snapshot_id)
        assert fetched.snapshot_id == snapshot.snapshot_id
        assert fetched.status == RootfsSnapshotStatus.COMPLETE

        # 3. Create a new sandbox restoring from the snapshot
        restored = sandbox_factory(rootfs_snapshot_source=snapshot_name)
        restored_running = wait_for_running(isola_client, restored.sandbox_id)

        # 4. Verify the restored sandbox has the marker file
        read_result = restored_running.commands.run("cat", "/root/sdk-restore-test.txt")
        assert read_result.exit_code == 0
        assert "sdk-restored-data-marker" in read_result.stdout

    def test_snapshot_and_restore_preserves_data(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        # 1. Create a sandbox and write a marker file
        sb = sandbox_factory()
        running = wait_for_running(isola_client, sb.sandbox_id)
        result = running.commands.run("sh", "-c", "echo 'restored-data-marker' > /root/restore-test.txt")
        assert result.exit_code == 0

        # 2. Create a rootfs snapshot through the Python SDK and wait for completion
        snapshot_name = f"e2e-restore-{sb.sandbox_id}"
        snapshot = isola_client.rootfs_snapshots.create(
            sandbox_id=sb.sandbox_id,
            snapshot_name=snapshot_name,
            max_wait_seconds=120,
        )

        assert snapshot.sandbox_id == sb.sandbox_id
        assert snapshot.snapshot_name == snapshot_name
        assert snapshot.status == RootfsSnapshotStatus.COMPLETE

        fetched = isola_client.rootfs_snapshots.get(snapshot.snapshot_id)
        assert fetched.snapshot_id == snapshot.snapshot_id
        assert fetched.status == RootfsSnapshotStatus.COMPLETE

        # 4. Create a new sandbox restoring from the snapshot
        restored = sandbox_factory(rootfs_snapshot_source=snapshot_name)
        restored_running = wait_for_running(isola_client, restored.sandbox_id)

        # 5. Verify the restored sandbox has the marker file
        read_result = restored_running.commands.run("cat", "/root/restore-test.txt")
        assert read_result.exit_code == 0
        assert "restored-data-marker" in read_result.stdout

        # 6. Verify the rootfs_snapshot_sources property
        assert restored_running.rootfs_snapshot_sources is not None
        assert len(restored_running.rootfs_snapshot_sources) == 1
        assert restored_running.rootfs_snapshot_sources[0].snapshot_name == snapshot_name
