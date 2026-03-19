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

import time

import pytest

from isola import Isola, IsolaError, NotFoundError, Sandbox, SandboxStatus

from conftest import wait_for_running


@pytest.mark.timeout(60)
def test_active_deadline_sandbox_stops(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with timeout should stop or be deleted after the deadline passes."""
    sb = sandbox_factory(image="alpine:3.21", timeout=10)
    wait_for_running(isola_client, sb.id)

    # Wait for the 10s deadline to fire + operator reconciliation.
    deadline = time.monotonic() + 30
    last_status = None
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.id)
        except NotFoundError:
            return
        last_status = current.status
        if current.status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
            return
        time.sleep(1.0)

    pytest.fail(
        f"Sandbox {sb.id} did not stop or disappear within 30s after reaching running "
        f"(last status: {last_status})"
    )


@pytest.mark.timeout(60)
def test_command_timeout(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """A command with timeout=3 should be killed after ~3 seconds with a non-zero exit code."""
    cmd = session_sandbox.commands.spawn("sleep", "300", timeout=3)

    # The command should still be running immediately after creation.
    assert cmd.exit_code() is None

    # Wait for the sidecar to kill the command after the timeout expires.
    code = cmd.wait()

    # A signal-killed process should have a non-zero exit code (typically 128+signal).
    assert code != 0, f"Expected non-zero exit code for timed-out command, got {code}"


@pytest.mark.timeout(30)
def test_no_deadline_stays_alive(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """A sandbox created without timeout should remain running and have no deadline."""
    sb = isola_client.sandboxes.get(session_sandbox.id)
    assert sb.status == SandboxStatus.RUNNING, (
        f"Expected sandbox without deadline to stay running, but status is {sb.status.value}"
    )
    assert sb.timeout is None, (
        f"Expected no deadline on session sandbox, got {sb.timeout}"
    )


@pytest.mark.timeout(60)
def test_operations_on_timed_out_sandbox(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """After a sandbox times out and stops/disappears, running a command on it should fail."""
    sb = sandbox_factory(image="alpine:3.21", timeout=10)
    running = wait_for_running(isola_client, sb.id)

    # Wait for the 10s deadline to fire + operator reconciliation.
    deadline = time.monotonic() + 30
    sandbox_gone = False
    last_status = None
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.id)
        except NotFoundError:
            sandbox_gone = True
            break
        last_status = current.status
        if current.status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
            sandbox_gone = True
            break
        time.sleep(1.0)

    assert sandbox_gone, (
        f"Sandbox {sb.id} did not reach a terminal state within 30s "
        f"(last status: {last_status})"
    )

    # Attempting to run a command on the stopped/deleted sandbox should raise an error.
    with pytest.raises((NotFoundError, IsolaError)):
        running.commands.spawn("echo", "should fail")
