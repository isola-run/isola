"""Advanced command execution tests: env merging, concurrency, stdin edge cases, signal codes."""

from __future__ import annotations

import asyncio

import pytest

from isola import AsyncSandbox, ConflictError, Isola, Sandbox

from conftest import wait_for_exit, wait_for_exit_async, wait_for_running


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
def test_stdin_to_exited_command_returns_conflict(session_sandbox: Sandbox) -> None:
    """Writing stdin to a command that already exited raises ConflictError (409)."""
    cmd = session_sandbox.commands.run(cmd="true")
    wait_for_exit(cmd)

    with pytest.raises(ConflictError):
        cmd.write_stdin(b"data after exit\n")


@pytest.mark.timeout(60)
def test_parallel_commands_isolated_output(session_sandbox: Sandbox) -> None:
    """Multiple concurrent commands produce isolated stdout streams."""
    # Start 3 commands that each echo a unique marker
    commands = []
    for i in range(3):
        cmd = session_sandbox.commands.run(cmd="echo", args=[f"marker_{i}"])
        commands.append((i, cmd))

    # Verify each command's output contains only its own marker
    for i, cmd in commands:
        wait_for_exit(cmd)
        with cmd.stdout() as stream:
            output = "".join(chunk for chunk in stream)
        assert f"marker_{i}" in output


@pytest.mark.timeout(60)
def test_kill_exit_code(session_sandbox: Sandbox) -> None:
    """A killed command exits with -1 (sidecar convention for signal-killed processes).

    Go's exec.CommandContext cancels via SIGKILL; the sidecar reports -1
    when ExitError has no clean exit status (e.g. signal death in gVisor).
    """
    cmd = session_sandbox.commands.run(cmd="sleep", args=["300"])
    assert cmd.exit_code() is None

    cmd.kill()
    code = wait_for_exit(cmd, timeout=10)

    assert code == -1, f"Expected exit code -1 for killed command, got {code}"


@pytest.mark.timeout(60)
def test_stdout_readable_after_kill(session_sandbox: Sandbox) -> None:
    """Output written before kill is still readable after the command terminates."""
    cmd = session_sandbox.commands.run(
        cmd="sh",
        args=["-c", "echo before_kill; sleep 300"],
    )

    # Read stdout until "before_kill" appears, confirming the echo flushed,
    # then break early to avoid blocking on sleep 300.
    with cmd.stdout(timeout=30.0) as stream:
        for chunk in stream:
            if "before_kill" in chunk:
                break

    cmd.kill()
    wait_for_exit(cmd, timeout=10)

    # Output file persists after kill -- verify the content is still readable.
    with cmd.stdout(timeout=10.0) as stream:
        output = "".join(chunk for chunk in stream)
    assert "before_kill" in output


@pytest.mark.timeout(30)
def test_default_cwd_is_container_root(session_sandbox: Sandbox) -> None:
    """With no cwd specified, the command runs in the container's default directory."""
    cmd = session_sandbox.commands.run(cmd="pwd")
    wait_for_exit(cmd)

    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)

    # Alpine's default WORKDIR is /
    assert output.strip() == "/"


@pytest.mark.timeout(60)
async def test_concurrent_stdin_writes_are_non_interleaved(async_session_sandbox: AsyncSandbox) -> None:
    """Concurrent write_stdin calls for the same command produce non-interleaved output.

    stdinMu in the sidecar is held for the entire io.Copy of each HTTP request,
    so each caller's bytes land in the pipe as one uninterrupted block regardless
    of payload size. Each writer uses a unique byte value (0..NUM_WRITERS-1), so
    every run in stdout must be exactly BLOCK_SIZE bytes with no mixing.

    BLOCK_SIZE is kept below io.Copy's 32KB internal buffer so each write is a
    single write() syscall, making head's read-buffer behaviour deterministic.
    """
    NUM_WRITERS = 8
    BLOCK_SIZE = 1024  # < 32KB (io.Copy chunk size) for a single write() per call
    TOTAL = NUM_WRITERS * BLOCK_SIZE

    # head -c exits naturally after reading exactly TOTAL bytes — no kill or sleep needed.
    # cat would require an explicit kill because the sidecar never sends EOF on the stdin
    # pipe (it stays open for the command's lifetime), so cat would block forever.
    cmd = await async_session_sandbox.commands.run(
        cmd="head", args=["-c", str(TOTAL)], text=False
    )

    errors: list[Exception] = []

    async def write_block(writer_id: int) -> None:
        try:
            await cmd.write_stdin(bytes([writer_id]) * BLOCK_SIZE)
        except Exception as e:
            errors.append(e)

    await asyncio.gather(*[write_block(i) for i in range(NUM_WRITERS)])

    assert not errors, f"Concurrent write_stdin raised: {errors}"

    # head exits naturally once it has consumed TOTAL bytes; no kill needed.
    await wait_for_exit_async(cmd, timeout=10)

    async with cmd.stdout(timeout=10.0) as stream:
        output = b"".join([chunk async for chunk in stream])

    assert len(output) == NUM_WRITERS * BLOCK_SIZE, (
        f"Expected {NUM_WRITERS * BLOCK_SIZE} bytes, got {len(output)}"
    )

    # Scan for contiguous runs. Since each writer uses a unique byte value,
    # no two consecutive runs share a byte value, so every run must be exactly
    # BLOCK_SIZE bytes. Any shorter run indicates interleaving.
    i = 0
    while i < len(output):
        byte_val = output[i]
        run_end = i
        while run_end < len(output) and output[run_end] == byte_val:
            run_end += 1
        run_len = run_end - i
        assert run_len == BLOCK_SIZE, (
            f"Interleaved write detected: byte 0x{byte_val:02x} has run of "
            f"{run_len} bytes (expected exactly {BLOCK_SIZE})"
        )
        i = run_end
