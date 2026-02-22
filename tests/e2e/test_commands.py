from __future__ import annotations

import pytest

from isola import Sandbox


def test_echo_stdout(session_sandbox: Sandbox) -> None:
    """Run echo and verify stdout contains the expected output."""
    result = session_sandbox.commands.run("echo", "hello world")

    assert "hello world" in result.stdout
    assert result.exit_code == 0


def test_command_with_args(session_sandbox: Sandbox) -> None:
    """Run printf with format args and verify exact stdout."""
    result = session_sandbox.commands.run("printf", "%s-%s", "foo", "bar")

    assert result.stdout == "foo-bar"


def test_command_with_env(session_sandbox: Sandbox) -> None:
    """Run a shell command that reads an environment variable."""
    result = session_sandbox.commands.run(
        "sh", "-c", "echo $MY_VAR",
        env={"MY_VAR": "test_value"},
    )

    assert "test_value" in result.stdout


def test_command_with_cwd(session_sandbox: Sandbox) -> None:
    """Run pwd with a specific working directory and verify output."""
    result = session_sandbox.commands.run("pwd", cwd="/tmp")

    assert result.stdout.strip() == "/tmp"


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
    result = session_sandbox.commands.run("sh", "-c", exit_arg)

    assert result.exit_code == expected_code


def test_exit_code_null_while_running(session_sandbox: Sandbox) -> None:
    """Verify exit_code() returns None for a command that is still running."""
    cmd = session_sandbox.commands.spawn("sleep", "30")
    try:
        code = cmd.exit_code()
        assert code is None
    finally:
        cmd.kill()


def test_write_stdin(session_sandbox: Sandbox) -> None:
    """Write to stdin and verify the command reads and echoes it."""
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "read line; echo \"got: $line\"",
    )
    cmd.write_stdin(b"hello from stdin\n")
    code = cmd.wait()

    assert code == 0

    output = cmd.stdout.read()
    assert "got: hello from stdin" in output


def test_kill_running_command(session_sandbox: Sandbox) -> None:
    """Kill a long-running command and verify it terminates with a non-None exit code."""
    cmd = session_sandbox.commands.spawn("sleep", "300", timeout=15)

    # Verify the command is running
    assert cmd.exit_code() is None

    cmd.kill()

    code = cmd.wait()
    assert code is not None


def test_kill_is_idempotent(session_sandbox: Sandbox) -> None:
    """Kill a command twice to verify idempotency -- second kill must not raise."""
    cmd = session_sandbox.commands.spawn("sleep", "300", timeout=15)
    cmd.kill()
    cmd.wait()

    # Second kill should not raise
    cmd.kill()


def test_stderr_output(session_sandbox: Sandbox) -> None:
    """Run a command that writes to stderr and verify the stderr stream."""
    result = session_sandbox.commands.run("sh", "-c", "echo error_msg >&2")

    assert "error_msg" in result.stderr


def test_run_with_input(session_sandbox: Sandbox) -> None:
    """Run a command with input= and verify it receives the data on stdin."""
    result = session_sandbox.commands.run("cat", input="hello\n")
    assert result.exit_code == 0
    assert result.stdout == "hello\n"
