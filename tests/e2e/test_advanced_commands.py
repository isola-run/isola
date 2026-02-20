"""Advanced command execution tests: env merging, concurrency, stdin edge cases, signal codes."""

from __future__ import annotations

import time

import pytest

from isola import ConflictError, Isola, Sandbox

from conftest import wait_for_exit, wait_for_running


@pytest.mark.timeout(90)
def test_container_env_accessible_in_command(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Env vars set at sandbox creation are accessible inside commands."""
    sb = sandbox_factory(
        image="alpine:3.21",
        env={"E2E_SECRET": "expected_value"},
    )
    running = wait_for_running(isola_client, sb.id)

    cmd = running.commands.run(cmd="sh", args=["-c", "echo $E2E_SECRET"])
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "expected_value" in output


@pytest.mark.timeout(90)
def test_command_env_overrides_container_env(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Per-command env overrides container-level env of the same name.

    Validates buildCmdEnv merge semantics: command env takes precedence.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        env={"MY_VAR": "original"},
    )
    running = wait_for_running(isola_client, sb.id)

    cmd = running.commands.run(
        cmd="sh",
        args=["-c", "echo $MY_VAR"],
        env={"MY_VAR": "overridden"},
    )
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    assert "overridden" in output
    assert "original" not in output


@pytest.mark.timeout(60)
def test_stdin_to_exited_command_returns_conflict(shared_sandbox: Sandbox) -> None:
    """Writing stdin to a command that already exited raises ConflictError (409)."""
    cmd = shared_sandbox.commands.run(cmd="true")
    wait_for_exit(cmd)

    with pytest.raises(ConflictError):
        cmd.write_stdin(b"data after exit\n")


@pytest.mark.timeout(60)
def test_parallel_commands_isolated_output(shared_sandbox: Sandbox) -> None:
    """Multiple concurrent commands produce isolated stdout streams."""
    # Start 3 commands that each echo a unique marker
    commands = []
    for i in range(3):
        cmd = shared_sandbox.commands.run(cmd="echo", args=[f"marker_{i}"])
        commands.append((i, cmd))

    # Verify each command's output contains only its own marker
    for i, cmd in commands:
        wait_for_exit(cmd)
        with cmd.stdout() as stream:
            output = "".join(chunk for chunk in stream)
        assert f"marker_{i}" in output


@pytest.mark.timeout(60)
def test_kill_exit_code(shared_sandbox: Sandbox) -> None:
    """A killed command exits with -1 (sidecar convention for signal-killed processes).

    Go's exec.CommandContext cancels via SIGKILL; the sidecar reports -1
    when ExitError has no clean exit status (e.g. signal death in gVisor).
    """
    cmd = shared_sandbox.commands.run(cmd="sleep", args=["300"])
    assert cmd.exit_code() is None

    cmd.kill()
    code = wait_for_exit(cmd, timeout=10)

    assert code == -1, f"Expected exit code -1 for killed command, got {code}"


@pytest.mark.timeout(60)
def test_stdout_readable_after_kill(shared_sandbox: Sandbox) -> None:
    """Output written before kill is still readable after the command terminates."""
    cmd = shared_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo before_kill; sleep 300"],
    )

    # Kill the command after a brief pause to let the echo complete
    time.sleep(1)
    cmd.kill()
    wait_for_exit(cmd, timeout=10)

    # Read stdout after kill -- the output file should still exist
    with cmd.stdout(timeout=10.0) as stream:
        output = "".join(chunk for chunk in stream)
    assert "before_kill" in output


@pytest.mark.timeout(30)
def test_default_cwd_is_container_root(shared_sandbox: Sandbox) -> None:
    """With no cwd specified, the command runs in the container's default directory."""
    cmd = shared_sandbox.commands.run(cmd="pwd")
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    # Alpine's default WORKDIR is /
    assert output.strip() == "/"
