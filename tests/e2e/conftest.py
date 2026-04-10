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

import warnings

import pytest
import pytest_asyncio

from isola import AsyncIsola, AsyncSandbox, Isola, NotFoundError, Sandbox

from utils import ISOLA_URL, wait_for_running, wait_for_running_async


def pytest_addoption(parser):
    parser.addoption("--slow", action="store_true", default=False, help="include @pytest.mark.slow tests")


def pytest_collection_modifyitems(config, items):
    if config.getoption("--slow"):
        return
    skip_slow = pytest.mark.skip(reason="need --slow option to run")
    for item in items:
        if item.get_closest_marker("slow"):
            item.add_marker(skip_slow)


# --- Sync fixtures ---


@pytest.fixture(scope="session")
def isola_client() -> Isola:
    client = Isola(url=ISOLA_URL)
    yield client
    client.close()


@pytest.fixture(scope="session")
def sandbox_factory(isola_client: Isola):
    created: list[str] = []

    def _create(**kwargs) -> Sandbox:
        if "image" not in kwargs and "containers" not in kwargs:
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
    client = AsyncIsola(url=ISOLA_URL)
    yield client
    await client.close()


@pytest_asyncio.fixture(loop_scope="session", scope="session")
async def async_sandbox_factory(async_isola_client: AsyncIsola):
    created: list[str] = []

    async def _create(**kwargs) -> AsyncSandbox:
        if "image" not in kwargs and "containers" not in kwargs:
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
