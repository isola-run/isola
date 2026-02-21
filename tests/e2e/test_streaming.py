from __future__ import annotations

import pytest

from isola import Sandbox


@pytest.mark.timeout(60)
def test_incremental_stdout(session_sandbox: Sandbox) -> None:
    """Output arrives progressively while the command is still running.

    The sleeps create observable time windows: the first chunk must arrive
    while the command is still sleeping (exit_code() is None), proving the
    stream does not buffer and flush only at exit.
    """
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo line1; sleep 0.5; echo line2; sleep 0.5; echo line3",
    )

    received_while_running = False
    collected: list[str] = []

    for chunk in cmd.stdout:
        collected.append(chunk)
        if not received_while_running and cmd.exit_code() is None:
            received_while_running = True

    output = "".join(collected)
    assert "line1\n" in output
    assert "line2\n" in output
    assert "line3\n" in output
    assert received_while_running, "Expected to receive output while command was still running"
    assert cmd.wait() == 0


@pytest.mark.timeout(60)
def test_stderr_stream(session_sandbox: Sandbox) -> None:
    """Stderr output is available through the stderr stream."""
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo err1 >&2; echo err2 >&2",
    )

    cmd.wait()

    output = cmd.stderr.read()

    assert "err1\n" in output
    assert "err2\n" in output


@pytest.mark.timeout(60)
def test_stream_completes_on_exit(session_sandbox: Sandbox) -> None:
    """A stream for a short-lived command exits cleanly when the command finishes."""
    cmd = session_sandbox.commands.spawn("echo", "done")

    output = cmd.stdout.read()

    assert "done\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(60)
def test_concurrent_stdout_stderr(session_sandbox: Sandbox) -> None:
    """Both stdout and stderr can be read from the same command."""
    cmd = session_sandbox.commands.run(
        "sh", "-c", "echo out; echo err >&2",
    )

    assert "out\n" in cmd.stdout.read()
    assert "err\n" in cmd.stderr.read()
