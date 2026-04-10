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

import pytest

from isola import BadRequestError, ConflictError, Sandbox


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
    cmd = session_sandbox.commands.spawn("sleep", "300", timeout_seconds=15)

    # Verify the command is running
    assert cmd.exit_code() is None

    cmd.kill()

    code = cmd.wait()
    assert code is not None


def test_kill_is_idempotent(session_sandbox: Sandbox) -> None:
    """Kill a command twice to verify idempotency -- second kill must not raise."""
    cmd = session_sandbox.commands.spawn("sleep", "300", timeout_seconds=15)
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


def test_command_no_output(session_sandbox: Sandbox) -> None:
    """A command that produces no output returns empty stdout and stderr."""
    result = session_sandbox.commands.run("true")

    assert result.exit_code == 0
    assert result.stdout == ""
    assert result.stderr == ""


def test_kill_already_exited_command(session_sandbox: Sandbox) -> None:
    """Killing a naturally-exited command should not raise -- kill is idempotent."""
    cmd = session_sandbox.commands.spawn("true")
    cmd.wait()

    # Command has already exited; kill should be a no-op
    cmd.kill()


def test_empty_stdin_write(session_sandbox: Sandbox) -> None:
    """Writing zero bytes to stdin should succeed without raising."""
    cmd = session_sandbox.commands.spawn("sleep", "30", timeout_seconds=15)
    try:
        cmd.write_stdin(b"")
    finally:
        cmd.kill()


def test_close_stdin_twice_raises_conflict(session_sandbox: Sandbox) -> None:
    """Closing stdin twice should raise ConflictError (409) on the second call."""
    cmd = session_sandbox.commands.spawn("sleep", "30", timeout_seconds=15)
    try:
        cmd.close_stdin()
        with pytest.raises(ConflictError):
            cmd.close_stdin()
    finally:
        cmd.kill()


def test_write_stdin_after_close_raises_conflict(session_sandbox: Sandbox) -> None:
    """Writing to stdin after it has been closed should raise ConflictError (409)."""
    cmd = session_sandbox.commands.spawn("sleep", "30", timeout_seconds=15)
    try:
        cmd.close_stdin()
        with pytest.raises(ConflictError):
            cmd.write_stdin(b"hello")
    finally:
        cmd.kill()


def test_isola_container_name_stripped(session_sandbox: Sandbox) -> None:
    """ISOLA_CONTAINER_NAME should not be visible inside user commands.

    The operator injects this env var into the container, but the sidecar strips
    it from the child process environment when executing user commands.
    """
    result = session_sandbox.commands.run("sh", "-c", 'echo "${ISOLA_CONTAINER_NAME}"')

    assert result.stdout.strip() == ""


def test_command_with_nonexistent_cwd(session_sandbox: Sandbox) -> None:
    """Running a command with a cwd that does not exist should raise BadRequestError."""
    with pytest.raises(BadRequestError) as exc_info:
        session_sandbox.commands.run("pwd", cwd="/nonexistent_path")
    assert exc_info.value.status_code == 400


def test_container_param_on_command(session_sandbox: Sandbox) -> None:
    """Explicitly targeting the primary container by name should work."""
    result = session_sandbox.commands.run("echo", "hello", container="sandbox0")

    assert result.exit_code == 0
    assert "hello" in result.stdout
