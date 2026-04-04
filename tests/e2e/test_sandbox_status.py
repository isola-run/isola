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

"""Sandbox status lifecycle tests.

Verifies the new sandbox status vocabulary:
  Pending -> Running -> Terminating (on delete)
  Pending -> Running -> Succeeded (clean exit)
  Pending -> Running -> Failed (non-zero exit / error)
"""

from __future__ import annotations

import time

import pytest

from isola import Isola, NotFoundError, SandboxStatus

from conftest import wait_for_running
from utils import POLL_INTERVAL, wait_for_status


class TestSandboxStatus:
    """Sandbox status state-machine tests."""

    @pytest.mark.timeout(90)
    def test_sandbox_reaches_running(
        self,
        isola_client: Isola,
        sandbox_factory,
    ) -> None:
        """A sandbox with a long-running command reaches Running status."""
        sb = sandbox_factory(
            image="alpine:3.21",
            command=["sleep", "infinity"],
        )
        running = wait_for_running(isola_client, sb.id)
        assert running.status == SandboxStatus.RUNNING

    @pytest.mark.timeout(180)
    def test_sandbox_reaches_succeeded_on_clean_exit(
        self,
        isola_client: Isola,
        sandbox_factory,
    ) -> None:
        """A sandbox whose command exits 0 transitions to Succeeded."""
        sb = sandbox_factory(
            image="alpine:3.21",
            command=["true"],
            max_wait_seconds=0,
        )
        result = wait_for_status(isola_client, sb.id, SandboxStatus.SUCCEEDED)
        assert result.status == SandboxStatus.SUCCEEDED

    @pytest.mark.timeout(180)
    def test_sandbox_reaches_failed_on_nonzero_exit(
        self,
        isola_client: Isola,
        sandbox_factory,
    ) -> None:
        """A sandbox whose command exits non-zero transitions to Failed."""
        sb = sandbox_factory(
            image="alpine:3.21",
            command=["sh", "-c", "exit 1"],
            max_wait_seconds=0,
        )
        result = wait_for_status(isola_client, sb.id, SandboxStatus.FAILED)
        assert result.status == SandboxStatus.FAILED

    @pytest.mark.timeout(120)
    def test_deleted_sandbox_shows_terminating_or_disappears(
        self,
        isola_client: Isola,
        sandbox_factory,
    ) -> None:
        """After deletion a Running sandbox shows Terminating or is already gone (404)."""
        sb = sandbox_factory(
            image="alpine:3.21",
            command=["sleep", "infinity"],
        )
        wait_for_running(isola_client, sb.id)

        running = isola_client.sandboxes.get(sb.id)
        running.delete()

        # Immediately check status after delete.
        try:
            current = isola_client.sandboxes.get(sb.id)
            assert current.status in (
                SandboxStatus.TERMINATING,
                SandboxStatus.RUNNING,
            ), (
                f"Expected Terminating or Running immediately after delete, "
                f"got {current.status.value}"
            )
        except NotFoundError:
            pass  # already gone -- acceptable

        # The sandbox should eventually disappear.
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                isola_client.sandboxes.get(sb.id)
            except NotFoundError:
                return
            time.sleep(POLL_INTERVAL)

        pytest.fail(f"Sandbox {sb.id} was not fully deleted within 30s after delete call")
