from __future__ import annotations

import time

import pytest

from isola import Sandbox

from conftest import wait_for_exit


def test_echo_stdout(session_sandbox: Sandbox) -> None:
    """Run echo and verify stdout contains the expected output."""
    cmd = session_sandbox.commands.run(cmd="echo", args=["hello world"])
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "hello world" in output


def test_command_with_args(session_sandbox: Sandbox) -> None:
    """Run printf with format args and verify exact stdout."""
    cmd = session_sandbox.commands.run(cmd="printf", args=["%s-%s", "foo", "bar"])
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert output == "foo-bar"


def test_command_with_env(session_sandbox: Sandbox) -> None:
    """Run a shell command that reads an environment variable."""
    cmd = session_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo $MY_VAR"],
        env={"MY_VAR": "test_value"},
    )
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "test_value" in output


def test_command_with_cwd(session_sandbox: Sandbox) -> None:
    """Run pwd with a specific working directory and verify output."""
    cmd = session_sandbox.commands.run(cmd="pwd", cwd="/tmp")
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert output.strip() == "/tmp"


@pytest.mark.parametrize(
    "exit_arg, expected_code",
    [
        ("exit 0", 0),
        ("exit 1", 1),
        ("exit 42", 42),
        ("exit 127", 127),
        ("exit 255", 255),
    ],
    ids=["zero", "one", "arbitrary", "command-not-found", "max-byte"],
)
def test_exit_code(session_sandbox: Sandbox, exit_arg: str, expected_code: int) -> None:
    """Verify exit codes are faithfully propagated through sidecar -> gateway -> SDK."""
    cmd = session_sandbox.commands.run(cmd="sh", args=["-c", exit_arg])
    code = wait_for_exit(cmd)

    assert code == expected_code


def test_exit_code_null_while_running(session_sandbox: Sandbox) -> None:
    """Verify exit_code() returns None for a command that is still running."""
    cmd = session_sandbox.commands.run(cmd="sleep", args=["30"])
    try:
        code = cmd.exit_code()
        assert code is None
    finally:
        cmd.kill()


def test_write_stdin(session_sandbox: Sandbox) -> None:
    """Write to stdin and verify the command reads and echoes it."""
    cmd = session_sandbox.commands.run(
        cmd="sh",
        args=["-c", "read line; echo \"got: $line\""],
    )
    cmd.write_stdin(b"hello from stdin\n")
    code = wait_for_exit(cmd)

    assert code == 0

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "got: hello from stdin" in output


def test_kill_running_command(session_sandbox: Sandbox) -> None:
    """Kill a long-running command and verify it terminates with a non-None exit code."""
    cmd = session_sandbox.commands.run(cmd="sleep", args=["300"])

    # Verify the command is running
    assert cmd.exit_code() is None

    cmd.kill()

    code = wait_for_exit(cmd, timeout=10)
    assert code is not None


def test_kill_is_idempotent(session_sandbox: Sandbox) -> None:
    """Kill a command twice to verify idempotency -- second kill must not raise."""
    cmd = session_sandbox.commands.run(cmd="sleep", args=["300"])
    cmd.kill()
    wait_for_exit(cmd, timeout=10)

    # Second kill should not raise
    cmd.kill()


def test_stderr_output(session_sandbox: Sandbox) -> None:
    """Run a command that writes to stderr and verify the stderr stream."""
    cmd = session_sandbox.commands.run(cmd="sh", args=["-c", "echo error_msg >&2"])
    wait_for_exit(cmd)

    with cmd.stderr() as stream:
        output = "".join(chunk for chunk in stream)

    assert "error_msg" in output


def test_binary_mode(session_sandbox: Sandbox) -> None:
    """Run a command in binary mode and verify stdout yields bytes, not str."""
    cmd = session_sandbox.commands.run(
        cmd="echo",
        args=["binary test"],
        text=False,
    )
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        chunks = list(stream)

    assert len(chunks) > 0
    for chunk in chunks:
        assert isinstance(chunk, bytes), f"Expected bytes, got {type(chunk).__name__}"

    combined = b"".join(chunks)
    assert b"binary test" in combined
