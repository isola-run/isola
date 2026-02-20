from __future__ import annotations

import pytest

from isola import Sandbox

from conftest import wait_for_exit


@pytest.mark.timeout(60)
def test_incremental_stdout(shared_sandbox: Sandbox) -> None:
    """Output produced in stages arrives as a complete stream."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo line1; sleep 0.5; echo line2; sleep 0.5; echo line3"],
        text=True,
    )

    with cmd.stdout(timeout=30.0) as stream:
        output = "".join(chunk for chunk in stream)

    assert "line1\n" in output
    assert "line2\n" in output
    assert "line3\n" in output
    assert wait_for_exit(cmd) == 0


@pytest.mark.timeout(60)
def test_stderr_stream(shared_sandbox: Sandbox) -> None:
    """Stderr output is available through the stderr stream."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo err1 >&2; echo err2 >&2"],
        text=True,
    )

    wait_for_exit(cmd)

    with cmd.stderr(timeout=30.0) as stream:
        output = "".join(chunk for chunk in stream)

    assert "err1\n" in output
    assert "err2\n" in output


@pytest.mark.timeout(60)
def test_offset_resume(shared_sandbox: Sandbox) -> None:
    """Reading with an offset skips the first N bytes of output."""
    cmd = shared_sandbox.commands.run(
        cmd="echo",
        args=["abcdefghij"],
        text=True,
    )

    wait_for_exit(cmd)

    # Full output: "abcdefghij\n" (11 bytes)
    with cmd.stdout(offset=0, timeout=30.0) as stream:
        full_output = "".join(chunk for chunk in stream)
    assert full_output == "abcdefghij\n"

    # Resume from byte 5: should skip "abcde" and return "fghij\n"
    with cmd.stdout(offset=5, timeout=30.0) as stream:
        resumed_output = "".join(chunk for chunk in stream)
    assert resumed_output == "fghij\n"


@pytest.mark.timeout(60)
def test_binary_mode_streaming(shared_sandbox: Sandbox) -> None:
    """Binary mode streams yield bytes, not str."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "printf '\\x00\\x01\\x02\\xff\\xfe'"],
        text=False,
    )

    wait_for_exit(cmd)

    with cmd.stdout(timeout=30.0) as stream:
        chunks = list(stream)

    assert len(chunks) > 0
    for chunk in chunks:
        assert isinstance(chunk, bytes)

    combined = b"".join(chunks)
    assert combined == b"\x00\x01\x02\xff\xfe"


@pytest.mark.timeout(60)
def test_stream_completes_on_exit(shared_sandbox: Sandbox) -> None:
    """A stream for a short-lived command exits cleanly when the command finishes."""
    cmd = shared_sandbox.commands.run(
        cmd="echo",
        args=["done"],
        text=True,
    )

    with cmd.stdout(timeout=30.0) as stream:
        output = "".join(chunk for chunk in stream)

    assert "done\n" in output
    assert wait_for_exit(cmd) == 0


@pytest.mark.timeout(60)
def test_concurrent_stdout_stderr(shared_sandbox: Sandbox) -> None:
    """Both stdout and stderr can be read from the same command."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo out; echo err >&2"],
        text=True,
    )

    wait_for_exit(cmd)

    with cmd.stdout(timeout=30.0) as stream:
        stdout_output = "".join(chunk for chunk in stream)

    with cmd.stderr(timeout=30.0) as stream:
        stderr_output = "".join(chunk for chunk in stream)

    assert "out\n" in stdout_output
    assert "err\n" in stderr_output
