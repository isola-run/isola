"""Advanced command execution tests: env merging, concurrency, stdin edge cases, signal codes."""

from __future__ import annotations

import asyncio
import time

import pytest

from isola import AsyncSandbox, ConflictError, Isola, Sandbox

from conftest import wait_for_running


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

    cmd = running.commands.run("sh", "-c", "echo $E2E_SECRET")

    assert "expected_value" in cmd.stdout.read()


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
        "sh", "-c", "echo $MY_VAR",
        env={"MY_VAR": "overridden"},
    )

    output = cmd.stdout.read()
    assert "overridden" in output
    assert "original" not in output


@pytest.mark.timeout(60)
def test_stdin_to_exited_command_returns_conflict(session_sandbox: Sandbox) -> None:
    """Writing stdin to a command that already exited raises ConflictError (409)."""
    cmd = session_sandbox.commands.spawn("true")
    cmd.wait()

    with pytest.raises(ConflictError):
        cmd.write_stdin(b"data after exit\n")


@pytest.mark.timeout(60)
def test_parallel_commands_isolated_output(session_sandbox: Sandbox) -> None:
    """Multiple concurrent commands produce isolated stdout streams."""
    # Start 3 commands that each echo a unique marker
    cmds = []
    for i in range(3):
        cmd = session_sandbox.commands.spawn("echo", f"marker_{i}")
        cmds.append((i, cmd))

    # Verify each command's output contains only its own marker
    for i, cmd in cmds:
        cmd.wait()
        output = cmd.stdout.read()
        assert f"marker_{i}" in output


@pytest.mark.timeout(60)
def test_kill_exit_code(session_sandbox: Sandbox) -> None:
    """A killed command exits with -1 (sidecar convention for signal-killed processes).

    Go's exec.CommandContext cancels via SIGKILL; the sidecar reports -1
    when ExitError has no clean exit status (e.g. signal death in gVisor).
    """
    cmd = session_sandbox.commands.spawn("sleep", "300")
    assert cmd.exit_code() is None

    cmd.kill()
    code = cmd.wait(timeout=10)

    assert code == -1, f"Expected exit code -1 for killed command, got {code}"


@pytest.mark.timeout(60)
def test_stdout_readable_after_kill(session_sandbox: Sandbox) -> None:
    """Output written before kill is still readable after the command terminates."""
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo before_kill; sleep 300",
    )

    # Give the echo time to execute and flush
    time.sleep(1)

    cmd.kill()
    cmd.wait(timeout=10)

    # Output file persists after kill -- verify the content is still readable.
    output = cmd.stdout.read()
    assert "before_kill" in output


@pytest.mark.timeout(30)
def test_default_cwd_is_container_root(session_sandbox: Sandbox) -> None:
    """With no cwd specified, the command runs in the container's default directory."""
    cmd = session_sandbox.commands.run("pwd")

    # Alpine's default WORKDIR is /
    assert cmd.stdout.read().strip() == "/"


@pytest.mark.timeout(60)
async def test_concurrent_stdin_writes_are_non_interleaved(async_session_sandbox: AsyncSandbox) -> None:
    """Concurrent write_stdin calls for the same command produce non-interleaved output.

    stdinMu in the sidecar is held for the entire io.Copy of each HTTP request,
    so each caller's chars land in the pipe as one uninterrupted block regardless
    of payload size. Each writer uses a unique printable ASCII char ('A'..'H'), so
    every run in stdout must be exactly BLOCK_SIZE chars with no mixing.

    BLOCK_SIZE is kept below io.Copy's 32KB internal buffer so each write is a
    single write() syscall, making head's read-buffer behaviour deterministic.
    """
    NUM_WRITERS = 8
    BLOCK_SIZE = 1024  # < 32KB (io.Copy chunk size) for a single write() per call
    TOTAL = NUM_WRITERS * BLOCK_SIZE

    # head -c exits naturally after reading exactly TOTAL bytes — no kill or sleep needed.
    # cat would require an explicit kill because the sidecar never sends EOF on the stdin
    # pipe (it stays open for the command's lifetime), so cat would block forever.
    cmd = await async_session_sandbox.commands.spawn(
        "head", "-c", str(TOTAL),
    )

    errors: list[Exception] = []

    async def write_block(writer_id: int) -> None:
        try:
            await cmd.write_stdin(chr(ord('A') + writer_id) * BLOCK_SIZE)
        except Exception as e:
            errors.append(e)

    await asyncio.gather(*[write_block(i) for i in range(NUM_WRITERS)])

    assert not errors, f"Concurrent write_stdin raised: {errors}"

    # head exits naturally once it has consumed TOTAL bytes; no kill needed.
    await cmd.wait(timeout=10)

    output = await cmd.stdout.read()

    assert len(output) == NUM_WRITERS * BLOCK_SIZE, (
        f"Expected {NUM_WRITERS * BLOCK_SIZE} chars, got {len(output)}"
    )

    # Scan for contiguous runs. Since each writer uses a unique char,
    # no two consecutive runs share a char, so every run must be exactly
    # BLOCK_SIZE chars. Any shorter run indicates interleaving.
    i = 0
    while i < len(output):
        char_val = output[i]
        run_end = i
        while run_end < len(output) and output[run_end] == char_val:
            run_end += 1
        run_len = run_end - i
        assert run_len == BLOCK_SIZE, (
            f"Interleaved write detected: char {char_val!r} has run of "
            f"{run_len} chars (expected exactly {BLOCK_SIZE})"
        )
        i = run_end


@pytest.mark.timeout(60)
def test_close_stdin_unblocks_cat(session_sandbox: Sandbox) -> None:
    """Closing stdin sends EOF, allowing cat to exit."""
    cmd = session_sandbox.commands.spawn("cat")
    cmd.write_stdin("hello\n")
    cmd.close_stdin()
    code = cmd.wait(timeout=10)

    assert code == 0
    assert cmd.stdout.read() == "hello\n"
