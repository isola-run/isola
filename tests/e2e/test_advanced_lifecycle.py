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

"""Advanced lifecycle tests: multi-sandbox, sandbox-with-command deletion."""

from __future__ import annotations

import time

import pytest

from isola import (
    Isola,
    NotFoundError,
    SandboxStatus,
)

from conftest import wait_for_running

POLL_INTERVAL = 0.5


@pytest.mark.timeout(120)
def test_multiple_sandboxes_coexist(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Three sandboxes can run concurrently and all appear in list."""
    sandboxes = []
    for _ in range(3):
        sb = sandbox_factory(image="alpine:3.21")
        sandboxes.append(sb)

    # Wait for all to reach running
    for sb in sandboxes:
        wait_for_running(isola_client, sb.sandbox_id)

    summaries = isola_client.sandboxes.list()
    listed_ids = {s.sandbox_id for s in summaries}

    for sb in sandboxes:
        assert sb.sandbox_id in listed_ids, f"Sandbox {sb.sandbox_id} not found in list"


@pytest.mark.timeout(120)
def test_delete_sandbox_with_running_command(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Deleting a sandbox while a command is running should succeed.

    The finalizer cleans up the pod; the running command is lost.
    """
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.sandbox_id)

    # Start a long-running command
    running.commands.spawn("sleep", "300")

    # Delete the sandbox without killing the command
    running.delete()

    # The sandbox should eventually disappear
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            isola_client.sandboxes.get(sb.sandbox_id)
        except NotFoundError:
            return
        time.sleep(POLL_INTERVAL)

    pytest.fail(f"Sandbox {sb.sandbox_id} was not deleted within 30s after delete call")


@pytest.mark.timeout(180)
def test_short_lived_command_sandbox_stops(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with command=["true"] should transition to stopped after the process exits.

    Tests the pod-terminated path (PodSucceeded phase -> stopped status).
    """
    sb = sandbox_factory(image="alpine:3.21", command=["true"], max_wait_seconds=0)

    # Wait for the sandbox to reach a terminal state
    deadline = time.monotonic() + 120
    last_status = None
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.sandbox_id)
            last_status = current.status
            if last_status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
                return
        except NotFoundError:
            return  # deleted after stopping
        time.sleep(POLL_INTERVAL)

    pytest.fail(
        f"Sandbox {sb.sandbox_id} with command=['true'] did not reach terminal state "
        f"within 120s (last: {last_status})"
    )


@pytest.mark.timeout(180)
def test_crashed_container_sandbox_fails(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox whose container exits non-zero should transition to failed.

    Verifies that the native sidecar (init container with restartPolicy: Always)
    does not keep the pod alive in a zombie Running state. The kubelet should
    tear down the sidecar when all regular containers terminate, and the pod
    should reach Failed phase.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        command=["sh", "-c", "sleep 3; exit 1"],
    )

    deadline = time.monotonic() + 120
    last_status = None
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.sandbox_id)
            last_status = current.status
            if last_status == SandboxStatus.FAILED:
                return
            if last_status == SandboxStatus.STOPPED:
                pytest.fail(
                    f"Sandbox {sb.sandbox_id} reached 'stopped' but expected 'failed' "
                    f"(container exited non-zero)"
                )
        except NotFoundError:
            return  # deleted after failing
        time.sleep(POLL_INTERVAL)

    pytest.fail(
        f"Sandbox {sb.sandbox_id} with exit 1 did not reach 'failed' state "
        f"within 120s (last: {last_status})"
    )


@pytest.mark.timeout(30)
def test_list_returns_list_not_null(isola_client: Isola) -> None:
    """sandboxes.list() always returns a list, never None."""
    result = isola_client.sandboxes.list()
    assert isinstance(result, list)
