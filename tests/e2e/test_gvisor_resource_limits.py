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

"""Tests demonstrating gVisor per-container resource limit behavior.

gVisor runs all containers in a pod within a single sentry process. Per-container
cgroups are created at the host level for compatibility (e.g. cAdvisor discovery)
but have empty resources -- container-level memory and ephemeral-storage limits are
NOT enforced by gVisor. Only the pod-level cgroup (applied to the sentry process by
the host kernel) is real.

Since the sandbox-sidecar has no resource limits, sandbox pods are Burstable QoS,
meaning the pod-level cgroup also has no meaningful memory limit. The net effect:
a container can allocate memory or disk well beyond its declared limits without
being OOM-killed or evicted.
"""

from __future__ import annotations

import pytest

from isola import Isola, Sandbox

from conftest import wait_for_running


@pytest.mark.timeout(120)
def test_memory_limit_not_enforced_by_gvisor(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A container can exceed its declared memory limit under gVisor.

    With runc, writing 100MB to tmpfs in a container with a 64Mi memory limit
    triggers an OOM kill (exit code 137). With gVisor, the compat cgroup has no
    enforcement, so the write succeeds.
    """
    memory_limit = "64Mi"

    sb = sandbox_factory(image="alpine:3.21", memory=memory_limit, ephemeral_storage="64Mi")
    running = wait_for_running(isola_client, sb.sandbox_id)

    # Verify the limits are reported in the API response (set, just not enforced)
    container = running._data.pod_template.container
    assert container.resources is not None
    assert container.resources.limits is not None
    assert container.resources.limits.memory == memory_limit
    assert container.resources.limits.ephemeral_storage == "64Mi"

    # Write 100MB to /dev/shm (tmpfs, backed by memory).
    # This exceeds the 64Mi container limit by ~56%.
    # Under runc: OOM kill (exit 137). Under gVisor: succeeds.
    result = running.commands.run(
        "dd", "if=/dev/zero", "of=/dev/shm/bigfile", "bs=1M", "count=100"
    )
    assert result.exit_code == 0, (
        f"Expected dd to succeed (gVisor does not enforce per-container memory "
        f"limits), but got exit code {result.exit_code}. stderr: {result.stderr}"
    )

    # Confirm the full 100MB was actually written
    result = running.commands.run("stat", "-c", "%s", "/dev/shm/bigfile")
    assert result.exit_code == 0
    file_size = int(result.stdout.strip())
    assert file_size == 100 * 1024 * 1024, (
        f"Expected 100MB file but got {file_size} bytes"
    )


@pytest.mark.timeout(120)
def test_ephemeral_storage_limit_not_enforced_by_gvisor(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A container can exceed its declared ephemeral-storage limit under gVisor.

    With runc, the kubelet's eviction manager periodically checks disk usage
    against ephemeral-storage limits and evicts pods that exceed them. With gVisor,
    the sentry's internal filesystem writes are not visible to the kubelet's
    disk usage checks on the container's rootfs, so the limit is not enforced.
    """
    storage_limit = "64Mi"

    sb = sandbox_factory(image="alpine:3.21", ephemeral_storage=storage_limit)
    running = wait_for_running(isola_client, sb.sandbox_id)

    # Verify the limit is reported in the API response
    container = running._data.pod_template.container
    assert container.resources is not None
    assert container.resources.limits is not None
    assert container.resources.limits.ephemeral_storage == storage_limit

    # Write 100MB to the container's rootfs (not /tmp or /dev/shm which may be tmpfs).
    # Under runc: kubelet eviction manager would eventually evict the pod.
    # Under gVisor: the sentry manages its own filesystem; kubelet can't see usage.
    result = running.commands.run(
        "dd", "if=/dev/zero", "of=/bigfile", "bs=1M", "count=100"
    )
    assert result.exit_code == 0, (
        f"Expected dd to succeed (gVisor does not enforce per-container "
        f"ephemeral-storage limits), but got exit code {result.exit_code}. "
        f"stderr: {result.stderr}"
    )

    # Confirm the full 100MB was actually written to the rootfs
    result = running.commands.run("stat", "-c", "%s", "/bigfile")
    assert result.exit_code == 0
    file_size = int(result.stdout.strip())
    assert file_size == 100 * 1024 * 1024, (
        f"Expected 100MB file but got {file_size} bytes"
    )
