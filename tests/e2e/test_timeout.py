from __future__ import annotations

import time

import pytest

from isola import Isola, IsolaError, NotFoundError, Sandbox, SandboxStatus

from conftest import wait_for_exit, wait_for_running


@pytest.mark.timeout(180)
def test_active_deadline_sandbox_stops(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with active_deadline_seconds should stop or be deleted after the deadline passes."""
    sb = sandbox_factory(image="alpine:3.21", active_deadline_seconds=30)
    wait_for_running(isola_client, sb.id)

    # Wait up to 60s beyond when the sandbox became running for it to reach a
    # terminal state (stopped, failed) or be deleted entirely (NotFoundError).
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.id)
        except NotFoundError:
            return
        if current.status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
            return
        time.sleep(1.0)

    pytest.fail(
        f"Sandbox {sb.id} did not stop or disappear within 60s after reaching running "
        f"(last status: {current.status.value})"
    )


@pytest.mark.timeout(60)
def test_command_timeout(
    isola_client: Isola,
    shared_sandbox: Sandbox,
) -> None:
    """A command with timeout=3 should be killed after ~3 seconds with a non-zero exit code."""
    cmd = shared_sandbox.commands.run(cmd="sleep", args=["300"], timeout=3)

    # The command should still be running immediately after creation.
    assert cmd.exit_code() is None

    # Wait for the sidecar to kill the command after the timeout expires.
    code = wait_for_exit(cmd, timeout=15)

    # A signal-killed process should have a non-zero exit code (typically 128+signal).
    assert code != 0, f"Expected non-zero exit code for timed-out command, got {code}"


@pytest.mark.timeout(30)
def test_no_deadline_stays_alive(
    isola_client: Isola,
    shared_sandbox: Sandbox,
) -> None:
    """A sandbox created without active_deadline_seconds should remain running after a brief wait."""
    time.sleep(5)

    sb = isola_client.sandboxes.get(shared_sandbox.id)
    assert sb.status == SandboxStatus.RUNNING, (
        f"Expected sandbox without deadline to stay running, but status is {sb.status.value}"
    )


@pytest.mark.timeout(180)
def test_operations_on_timed_out_sandbox(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """After a sandbox times out and stops/disappears, running a command on it should fail."""
    sb = sandbox_factory(image="alpine:3.21", active_deadline_seconds=30)
    running = wait_for_running(isola_client, sb.id)

    # Wait for the sandbox to stop or be deleted.
    deadline = time.monotonic() + 60
    sandbox_gone = False
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.id)
        except NotFoundError:
            sandbox_gone = True
            break
        if current.status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
            sandbox_gone = True
            break
        time.sleep(1.0)

    assert sandbox_gone, (
        f"Sandbox {sb.id} did not reach a terminal state within 60s "
        f"(last status: {current.status.value})"
    )

    # Attempting to run a command on the stopped/deleted sandbox should raise an error.
    with pytest.raises((NotFoundError, IsolaError)):
        running.commands.run(cmd="echo", args=["should fail"])
