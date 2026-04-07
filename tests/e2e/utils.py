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

import pytest

from isola import AsyncIsola, AsyncSandbox, Isola, NotFoundError, Sandbox, SandboxStatus

SANDBOXES_NAMESPACE = "isola-sandboxes"
ISOLA_URL = os.environ.get("ISOLA_URL", "http://localhost:8080")
ISOLA_METRICS_URL = os.environ.get("ISOLA_METRICS_URL", "http://localhost:8082")

POLL_INTERVAL = 1.0
POLL_TIMEOUT = 90


# --- Sync helpers ---


def wait_for_running(client: Isola, sandbox_id: str, timeout: float = POLL_TIMEOUT) -> Sandbox:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        try:
            sb = client.sandboxes.get(sandbox_id)
        except NotFoundError:
            # Sandbox may not be visible in the api-gateway's K8s cache yet.
            time.sleep(POLL_INTERVAL)
            continue
        last_status = sb.status
        if sb.status == SandboxStatus.RUNNING:
            return sb
        if sb.status in (SandboxStatus.FAILED, SandboxStatus.SUCCEEDED):
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
        try:
            sb = client.sandboxes.get(sandbox_id)
        except NotFoundError:
            time.sleep(POLL_INTERVAL)
            continue
        last_status = sb.status
        if sb.status == target:
            return sb
        time.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not reach {target.value} within {timeout}s (last: {last_status})")


def wait_for_visible(client: Isola, sandbox_id: str, timeout: float = POLL_TIMEOUT) -> Sandbox:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            return client.sandboxes.get(sandbox_id)
        except NotFoundError:
            time.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not become visible within {timeout}s")


# --- Async helpers ---


async def wait_for_running_async(
    client: AsyncIsola, sandbox_id: str, timeout: float = POLL_TIMEOUT
) -> AsyncSandbox:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        try:
            sb = await client.sandboxes.get(sandbox_id)
        except NotFoundError:
            await asyncio.sleep(POLL_INTERVAL)
            continue
        last_status = sb.status
        if sb.status == SandboxStatus.RUNNING:
            return sb
        if sb.status in (SandboxStatus.FAILED, SandboxStatus.SUCCEEDED):
            pytest.fail(f"Sandbox {sandbox_id} reached terminal status: {sb.status.value}")
        await asyncio.sleep(POLL_INTERVAL)
    pytest.fail(f"Sandbox {sandbox_id} did not reach running within {timeout}s (last status: {last_status})")


# --- Parsing helpers ---


def parse_k8s_quantity(q: str) -> float:
    """Parse a Kubernetes resource quantity string into a base numeric value.

    Handles binary suffixes (Ki, Mi, Gi, Ti) and decimal suffixes (m, k, M, G, T).
    Returns bytes for memory-like quantities, cores for CPU.
    """
    suffixes = {
        "m": 1e-3,
        "k": 1e3,
        "M": 1e6,
        "G": 1e9,
        "T": 1e12,
        "Ki": 1024,
        "Mi": 1024**2,
        "Gi": 1024**3,
        "Ti": 1024**4,
    }
    for suffix, multiplier in sorted(suffixes.items(), key=lambda x: -len(x[0])):
        if q.endswith(suffix):
            return float(q[: -len(suffix)]) * multiplier
    return float(q)
