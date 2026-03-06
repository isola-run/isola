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

import asyncio
import os
import time
import warnings

import pytest
import pytest_asyncio

from isola import AsyncIsola, AsyncSandbox, Isola, NotFoundError, Sandbox, SandboxStatus

ISOLA_BASE_URL = os.environ.get("ISOLA_BASE_URL", "http://localhost:8080")
ISOLA_METRICS_URL = os.environ.get("ISOLA_METRICS_URL", "http://localhost:8082")

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
        except NotFoundError:
            pass  # already deleted by the test
        except Exception as e:
            warnings.warn(f"Failed to delete sandbox {sid} during teardown: {e}")


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
        except NotFoundError:
            pass  # already deleted by the test
        except Exception as e:
            warnings.warn(f"Failed to delete sandbox {sid} during teardown: {e}")


@pytest_asyncio.fixture(loop_scope="session", scope="session")
async def async_session_sandbox(async_isola_client: AsyncIsola, async_sandbox_factory) -> AsyncSandbox:
    sb = await async_sandbox_factory(image="alpine:3.21")
    return await wait_for_running_async(async_isola_client, sb.id)
