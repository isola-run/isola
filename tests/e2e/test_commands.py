from __future__ import annotations

import time

import pytest

from isola import Sandbox

from conftest import wait_for_exit


@pytest.mark.smoke
def test_echo_stdout(shared_sandbox: Sandbox) -> None:
    """Run echo and verify stdout contains the expected output."""
    cmd = shared_sandbox.commands.run(cmd="echo", args=["hello world"])
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "hello world" in output


def test_command_with_args(shared_sandbox: Sandbox) -> None:
    """Run printf with format args and verify exact stdout."""
    cmd = shared_sandbox.commands.run(cmd="printf", args=["%s-%s", "foo", "bar"])
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert output == "foo-bar"


def test_command_with_env(shared_sandbox: Sandbox) -> None:
    """Run a shell command that reads an environment variable."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo $MY_VAR"],
        env={"MY_VAR": "test_value"},
    )
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "test_value" in output


def test_command_with_cwd(shared_sandbox: Sandbox) -> None:
    """Run pwd with a specific working directory and verify output."""
    cmd = shared_sandbox.commands.run(cmd="pwd", cwd="/tmp")
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert output.strip() == "/tmp"


def test_exit_code_success(shared_sandbox: Sandbox) -> None:
    """Run true and verify exit code is 0."""
    cmd = shared_sandbox.commands.run(cmd="true")
    code = wait_for_exit(cmd)

    assert code == 0


def test_exit_code_failure(shared_sandbox: Sandbox) -> None:
    """Run a command that exits with code 42 and verify the exit code."""
    cmd = shared_sandbox.commands.run(cmd="sh", args=["-c", "exit 42"])
    code = wait_for_exit(cmd)

    assert code == 42


def test_exit_code_null_while_running(shared_sandbox: Sandbox) -> None:
    """Verify exit_code() returns None for a command that is still running."""
    cmd = shared_sandbox.commands.run(cmd="sleep", args=["30"])
    try:
        code = cmd.exit_code()
        assert code is None
    finally:
        cmd.kill()


def test_write_stdin(shared_sandbox: Sandbox) -> None:
    """Write to stdin and verify the command reads and echoes it."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "read line; echo \"got: $line\""],
    )
    cmd.write_stdin(b"hello from stdin\n")
    code = wait_for_exit(cmd)

    assert code == 0

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "got: hello from stdin" in output


def test_kill_running_command(shared_sandbox: Sandbox) -> None:
    """Kill a long-running command and verify it terminates with a non-None exit code."""
    cmd = shared_sandbox.commands.run(cmd="sleep", args=["300"])

    # Verify the command is running
    assert cmd.exit_code() is None

    cmd.kill()

    code = wait_for_exit(cmd, timeout=10)
    assert code is not None


def test_kill_is_idempotent(shared_sandbox: Sandbox) -> None:
    """Kill a command twice to verify idempotency -- second kill must not raise."""
    cmd = shared_sandbox.commands.run(cmd="sleep", args=["300"])
    cmd.kill()
    wait_for_exit(cmd, timeout=10)

    # Second kill should not raise
    cmd.kill()


def test_stderr_output(shared_sandbox: Sandbox) -> None:
    """Run a command that writes to stderr and verify the stderr stream."""
    cmd = shared_sandbox.commands.run(cmd="sh", args=["-c", "echo error_msg >&2"])
    wait_for_exit(cmd)

    with cmd.stderr() as stream:
        output = "".join(chunk for chunk in stream)

    assert "error_msg" in output


def test_binary_mode(shared_sandbox: Sandbox) -> None:
    """Run a command in binary mode and verify stdout yields bytes, not str."""
    cmd = shared_sandbox.commands.run(
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
