from __future__ import annotations

import asyncio
import os
import time

import pytest
import pytest_asyncio

from isola import AsyncIsola, AsyncSandbox, Isola, Sandbox, SandboxStatus

ISOLA_BASE_URL = os.environ.get("ISOLA_BASE_URL", "http://localhost:8080")

POLL_INTERVAL = 1.0
POLL_TIMEOUT = 90


# --- Sync helpers ---


def wait_for_running(client: Isola, sandbox_id: str, timeout: float = POLL_TIMEOUT) -> Sandbox:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        sb = client.sandboxes.get(sandbox_id)
        last_status = sb.status
        if sb.status == SandboxStatus.RUNNING:
            return sb
        if sb.status in (SandboxStatus.FAILED, SandboxStatus.STOPPED):
            pytest.fail(f"Sandbox {sandbox_id} reached terminal status: {sb.status.value}")
        time.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not reach running within {timeout}s (last status: {last_status})")


def wait_for_status(
    client: Isola,
    sandbox_id: str,
    target: SandboxStatus,
    timeout: float = POLL_TIMEOUT,
) -> Sandbox:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        sb = client.sandboxes.get(sandbox_id)
        last_status = sb.status
        if sb.status == target:
            return sb
        time.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not reach {target.value} within {timeout}s (last: {last_status})")


def wait_for_exit(cmd, *, timeout: float = 30) -> int:
    """Poll exit_code() until the command finishes or timeout is reached."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        code = cmd.exit_code()
        if code is not None:
            return code
        time.sleep(0.5)
    raise TimeoutError(f"Command {cmd.id} did not exit within {timeout}s")


# --- Async helpers ---


async def wait_for_running_async(
    client: AsyncIsola, sandbox_id: str, timeout: float = POLL_TIMEOUT
) -> AsyncSandbox:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        sb = await client.sandboxes.get(sandbox_id)
        last_status = sb.status
        if sb.status == SandboxStatus.RUNNING:
            return sb
        if sb.status in (SandboxStatus.FAILED, SandboxStatus.STOPPED):
            pytest.fail(f"Sandbox {sandbox_id} reached terminal status: {sb.status.value}")
        await asyncio.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not reach running within {timeout}s (last status: {last_status})")


async def wait_for_exit_async(cmd: AsyncSandbox, *, timeout: float = 30) -> int:
    """Poll exit_code() until the command finishes or timeout is reached."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        code = await cmd.exit_code()
        if code is not None:
            return code
        await asyncio.sleep(0.5)
    raise TimeoutError(f"Command did not exit within {timeout}s")


# --- Sync fixtures ---


@pytest.fixture(scope="session")
def isola_client() -> Isola:
    client = Isola(base_url=ISOLA_BASE_URL)
    yield client
    client.close()


@pytest.fixture(scope="session")
def sandbox_factory(isola_client: Isola):
    created: list[str] = []

    def _create(**kwargs) -> Sandbox:
        if "image" not in kwargs:
            kwargs["image"] = "alpine:3.21"
        sb = isola_client.sandboxes.create(**kwargs)
        created.append(sb.id)
        return sb

    yield _create

    for sid in created:
        try:
            isola_client.sandboxes.get(sid).delete()
        except Exception:
            pass


@pytest.fixture(scope="session")
def session_sandbox(isola_client: Isola, sandbox_factory) -> Sandbox:
    sb = sandbox_factory(image="alpine:3.21")
    return wait_for_running(isola_client, sb.id)


# --- Async fixtures ---


@pytest_asyncio.fixture(loop_scope="session", scope="session")
async def async_isola_client() -> AsyncIsola:
    client = AsyncIsola(base_url=ISOLA_BASE_URL)
    yield client
    await client.close()


@pytest_asyncio.fixture(loop_scope="session", scope="session")
async def async_sandbox_factory(async_isola_client: AsyncIsola):
    created: list[str] = []

    async def _create(**kwargs) -> AsyncSandbox:
        if "image" not in kwargs:
            kwargs["image"] = "alpine:3.21"
        sb = await async_isola_client.sandboxes.create(**kwargs)
        created.append(sb.id)
        return sb

    yield _create

    for sid in created:
        try:
            sb = await async_isola_client.sandboxes.get(sid)
            await sb.delete()
        except Exception:
            pass


@pytest_asyncio.fixture(loop_scope="session", scope="session")
async def async_session_sandbox(async_isola_client: AsyncIsola, async_sandbox_factory) -> AsyncSandbox:
    sb = await async_sandbox_factory(image="alpine:3.21")
    return await wait_for_running_async(async_isola_client, sb.id)
