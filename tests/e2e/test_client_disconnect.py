"""Tests for client disconnect during long-polling status requests.

Verifies that when a client disconnects (via HTTP timeout) while the api-gateway
is long-polling the sidecar for command status, the system recovers gracefully:
no leaked goroutines, no stuck commands, and subsequent operations work normally.
"""

from __future__ import annotations

import httpx
import pytest

from isola import Sandbox

from conftest import ISOLA_BASE_URL


@pytest.mark.timeout(30)
def test_client_disconnect_during_long_poll(session_sandbox: Sandbox) -> None:
    """A client that disconnects during long-poll status does not break the command.

    Simulates the ctx.Done() path in sidecar GetCommandStatus by issuing
    a long-poll request (?waitSeconds=60) with a short HTTP read timeout (2s).
    The httpx client times out and closes the connection, triggering context
    cancellation through: httpx timeout → gateway ctx cancelled → sidecar ctx.Done().

    The api-gateway internally hits huma.Error502BadGateway("failed to reach sidecar")
    but the client never receives it — it already got an httpx.ReadTimeout.
    """
    cmd = session_sandbox.commands.spawn("sleep", "30")
    try:
        # Use raw httpx with a short read timeout to force a client-side disconnect
        # while the sidecar is blocking in the long-poll select.
        with httpx.Client() as raw_client:
            with pytest.raises(httpx.ReadTimeout):
                raw_client.get(
                    f"{ISOLA_BASE_URL}/sandboxes/{session_sandbox.id}"
                    f"/commands/{cmd.id}/status",
                    params={"waitSeconds": 60},
                    timeout=httpx.Timeout(5.0, read=2.0),
                )

        # After the disconnect, the command should still be running and usable.
        code = cmd.exit_code()
        assert code is None, f"Expected command still running, got exit_code={code}"

        # Kill and verify clean shutdown.
        cmd.kill()
        code = cmd.wait()
        assert code is not None
    finally:
        cmd.kill()


@pytest.mark.timeout(30)
def test_command_usable_after_multiple_disconnects(session_sandbox: Sandbox) -> None:
    """Multiple client disconnects during long-poll don't corrupt command state."""
    cmd = session_sandbox.commands.spawn("sleep", "30")
    try:
        with httpx.Client() as raw_client:
            for _ in range(3):
                with pytest.raises(httpx.ReadTimeout):
                    raw_client.get(
                        f"{ISOLA_BASE_URL}/sandboxes/{session_sandbox.id}"
                        f"/commands/{cmd.id}/status",
                        params={"waitSeconds": 60},
                        timeout=httpx.Timeout(5.0, read=1.0),
                    )

        # Command should still be alive and reporting correctly.
        code = cmd.exit_code()
        assert code is None

        cmd.kill()
        code = cmd.wait()
        assert code is not None
    finally:
        cmd.kill()
