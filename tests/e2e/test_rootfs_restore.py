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

import json
import subprocess
import time

import pytest

from isola import Isola, Sandbox, SandboxStatus

from conftest import wait_for_running, wait_for_status


class TestRootfsSnapshotSourcesField:
    """Verify the rootfsSnapshotSources field round-trips through the API."""

    def test_create_response_includes_rootfs_snapshot_sources(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        sb = sandbox_factory(snapshot="nonexistent-snap")
        assert sb.rootfs_snapshot_sources is not None
        assert len(sb.rootfs_snapshot_sources) == 1
        assert sb.rootfs_snapshot_sources[0].snapshot_key == "nonexistent-snap"

    def test_get_response_includes_rootfs_snapshot_sources(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        sb = sandbox_factory(snapshot="nonexistent-snap")
        fetched = isola_client.sandboxes.get(sb.id)
        assert fetched.rootfs_snapshot_sources is not None
        assert len(fetched.rootfs_snapshot_sources) == 1
        assert fetched.rootfs_snapshot_sources[0].snapshot_key == "nonexistent-snap"

    def test_create_without_snapshot_has_no_sources(
        self, session_sandbox: Sandbox
    ) -> None:
        sources = session_sandbox.rootfs_snapshot_sources
        assert sources is None or sources == []

    def test_sandbox_with_nonexistent_snapshot_fails(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        sb = sandbox_factory(snapshot="does-not-exist")
        failed = wait_for_status(isola_client, sb.id, SandboxStatus.FAILED, timeout=60)
        assert failed.status == SandboxStatus.FAILED


class TestRootfsRestoreWorkflow:
    """Full end-to-end: create sandbox, write data, snapshot, restore, verify."""

    def test_snapshot_and_restore_preserves_data(
        self, sandbox_factory: ..., isola_client: Isola
    ) -> None:
        # 1. Create a sandbox and write a marker file
        sb = sandbox_factory()
        running = wait_for_running(isola_client, sb.id)
        result = running.commands.run("sh", "-c", "echo 'restored-data-marker' > /tmp/restore-test.txt")
        assert result.exit_code == 0

        # 2. Create a RootfsSnapshot CR via kubectl
        snapshot_key = f"e2e-restore-{sb.id}"
        snapshot_cr = {
            "apiVersion": "sandbox.isola.run/v1alpha1",
            "kind": "RootfsSnapshot",
            "metadata": {
                "name": snapshot_key,
                "namespace": "isola-sandboxes",
            },
            "spec": {
                "sandboxName": sb.id,
                "snapshotName": snapshot_key,
                "ttlSecondsAfterFinished": 300,
            },
        }
        subprocess.run(
            ["kubectl", "apply", "-f", "-"],
            input=json.dumps(snapshot_cr),
            capture_output=True,
            check=True,
            text=True,
        )

        # 3. Wait for snapshot to complete
        _wait_for_snapshot_complete(snapshot_key, timeout=120)

        # 4. Create a new sandbox restoring from the snapshot
        restored = sandbox_factory(snapshot=snapshot_key)
        restored_running = wait_for_running(isola_client, restored.id)

        # 5. Verify the restored sandbox has the marker file
        read_result = restored_running.commands.run("cat", "/tmp/restore-test.txt")
        assert read_result.exit_code == 0
        assert "restored-data-marker" in read_result.stdout

        # 6. Verify the rootfs_snapshot_sources property
        assert restored_running.rootfs_snapshot_sources is not None
        assert len(restored_running.rootfs_snapshot_sources) == 1
        assert restored_running.rootfs_snapshot_sources[0].snapshot_key == snapshot_key


def _wait_for_snapshot_complete(snapshot_name: str, timeout: float = 120) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = subprocess.run(
            [
                "kubectl", "get", "rootfssnapshot", snapshot_name,
                "-n", "isola-sandboxes", "-o", "json",
            ],
            capture_output=True,
            text=True,
            check=True,
        )
        status = json.loads(result.stdout).get("status", {})
        conditions = status.get("conditions", [])
        complete = next((c for c in conditions if c["type"] == "Complete"), None)
        if complete and complete["status"] == "True":
            return
        failed = next((c for c in conditions if c["type"] == "Failed"), None)
        if failed and failed["status"] == "True":
            pytest.fail(f"Snapshot {snapshot_name} failed: {failed.get('message', 'unknown')}")
        time.sleep(2)
    pytest.fail(f"Snapshot {snapshot_name} did not complete within {timeout}s")
