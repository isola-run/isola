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
        wait_for_running(isola_client, sb.id)

    summaries = isola_client.sandboxes.list()
    listed_ids = {s.id for s in summaries}

    for sb in sandboxes:
        assert sb.id in listed_ids, f"Sandbox {sb.id} not found in list"


@pytest.mark.timeout(120)
def test_delete_sandbox_with_running_command(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Deleting a sandbox while a command is running should succeed.

    The finalizer cleans up the pod; the running command is lost.
    """
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.id)

    # Start a long-running command
    running.commands.spawn("sleep", "300")

    # Delete the sandbox without killing the command
    running.delete()

    # The sandbox should eventually disappear
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            isola_client.sandboxes.get(sb.id)
        except NotFoundError:
            return
        time.sleep(POLL_INTERVAL)

    pytest.fail(f"Sandbox {sb.id} was not deleted within 30s after delete call")


@pytest.mark.timeout(180)
def test_short_lived_command_sandbox_stops(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with command=["true"] should transition to stopped after the process exits.

    Tests the pod-terminated path (PodSucceeded phase -> stopped status).
    """
    sb = sandbox_factory(image="alpine:3.21", command=["true"])

    # Wait for the sandbox to reach a terminal state
    deadline = time.monotonic() + 120
    last_status = None
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(sb.id)
            last_status = current.status
            if last_status in (SandboxStatus.STOPPED, SandboxStatus.FAILED):
                return
        except NotFoundError:
            return  # deleted after stopping
        time.sleep(POLL_INTERVAL)

    pytest.fail(
        f"Sandbox {sb.id} with command=['true'] did not reach terminal state "
        f"within 120s (last: {last_status})"
    )


@pytest.mark.timeout(30)
def test_list_returns_list_not_null(isola_client: Isola) -> None:
    """sandboxes.list() always returns a list, never None."""
    result = isola_client.sandboxes.list()
    assert isinstance(result, list)
