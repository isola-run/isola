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

import time

import pytest

from isola import (
    BadGatewayError,
    BadRequestError,
    Isola,
    IsolaError,
    NetworkSpec,
    NotFoundError,
    Sandbox,
    SandboxStatus,
    ValidationError,
)
from isola._commands import Commands
from isola._filesystem import Filesystem

from utils import wait_for_running

FAKE_SANDBOX_ID = "nonexistent-sandbox-xyz"

POLL_INTERVAL = 0.5
POLL_TIMEOUT = 60


def test_get_nonexistent_sandbox(isola_client: Isola) -> None:
    """Getting a sandbox that does not exist should raise NotFoundError with status 404."""
    with pytest.raises(NotFoundError) as exc_info:
        isola_client.sandboxes.get(FAKE_SANDBOX_ID)

    assert exc_info.value.status_code == 404


def test_commands_on_nonexistent_sandbox(isola_client: Isola) -> None:
    """Running a command on a sandbox that does not exist should raise an error."""
    commands = Commands(isola_client._api, FAKE_SANDBOX_ID)

    with pytest.raises(IsolaError) as exc_info:
        commands.spawn("echo", "hello")

    assert exc_info.value.status_code >= 400


def test_filesystem_on_nonexistent_sandbox(isola_client: Isola) -> None:
    """Reading a file from a sandbox that does not exist should raise an error."""
    filesystem = Filesystem(isola_client._api, FAKE_SANDBOX_ID)

    with pytest.raises(IsolaError) as exc_info:
        filesystem.read("/tmp/anything.txt")

    assert exc_info.value.status_code >= 400


@pytest.mark.skip(
    reason="OPERATOR BUG: Pod with ImagePullBackOff stays in Pending phase but operator "
    "only checks IsPodTerminated (Succeeded/Failed). Sandbox stays in 'creating' forever. "
    "Operator needs to detect persistent image pull failures from container status."
)
@pytest.mark.timeout(90)
def test_invalid_image_sandbox_fails(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Creating a sandbox with a nonexistent image should eventually reach failed status.

    The sandbox CR is accepted (create returns 201), but the pod fails to pull
    the image and the sandbox transitions to a terminal failure state.
    """
    sb = sandbox_factory(image="invalid-image-that-does-not-exist:99.99")
    assert sb.id

    deadline = time.monotonic() + POLL_TIMEOUT
    last_status = sb.status
    while time.monotonic() < deadline:
        sb_fresh = isola_client.sandboxes.get(sb.id)
        last_status = sb_fresh.status
        if last_status in (SandboxStatus.FAILED, SandboxStatus.STOPPED):
            return
        time.sleep(POLL_INTERVAL)

    pytest.fail(
        f"Sandbox {sb.id} with invalid image did not reach failed/stopped "
        f"within {POLL_TIMEOUT}s (last status: {last_status.value})"
    )


@pytest.mark.timeout(90)
def test_delete_already_deleted(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Deleting a sandbox twice should not raise -- the DELETE endpoint is idempotent."""
    sb = sandbox_factory(image="alpine:3.21")
    wait_for_running(isola_client, sb.id)

    sb_fresh = isola_client.sandboxes.get(sb.id)

    # First delete
    sb_fresh.delete()

    # Second delete should succeed without raising
    sb_fresh.delete()


def test_empty_image_validation(isola_client: Isola) -> None:
    """Creating a sandbox with an empty image string should be rejected by the server.

    The api-gateway should return a 400 or 422 before the resource is created.
    """
    with pytest.raises((ValidationError, BadRequestError)) as exc_info:
        isola_client.sandboxes.create(image="")

    assert exc_info.value.status_code in (400, 422)


@pytest.mark.timeout(90)
def test_commands_on_deleted_sandbox(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Running a command on a sandbox that has been deleted should raise an error.

    The exact error depends on timing: the sandbox K8s resource may already be gone
    (NotFoundError) or the sidecar may be unreachable (BadGatewayError).
    """
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.id)

    # Grab a reference to commands before deleting
    commands = Commands(isola_client._api, running.id)

    # Delete the sandbox
    running.delete()

    # Poll until the sandbox is gone or no longer running
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            current = isola_client.sandboxes.get(running.id)
            if current.status != SandboxStatus.RUNNING:
                break
        except NotFoundError:
            break
        time.sleep(0.5)

    with pytest.raises(IsolaError):
        commands.spawn("echo", "should fail")


@pytest.mark.timeout(90)
def test_invalid_command_nonzero_exit(session_sandbox: Sandbox) -> None:
    """Running a binary that does not exist inside the sandbox should produce a non-zero exit code.

    The sidecar accepts the command (202), but fails to exec the binary,
    resulting in a non-zero exit code.
    """
    result = session_sandbox.commands.run("/usr/bin/nonexistent_binary_xyz")

    assert result.exit_code != 0, f"Expected non-zero exit code, got {result.exit_code}"


def test_zero_timeout_rejected(isola_client: Isola) -> None:
    """Creating a sandbox with timeout_seconds=0 should be rejected -- the minimum is 1.

    The api-gateway enforces timeoutSeconds >= 1 via Huma field validation.
    """
    with pytest.raises((ValidationError, BadRequestError)) as exc_info:
        isola_client.sandboxes.create(image="alpine:3.21", timeout_seconds=0)

    assert exc_info.value.status_code in (400, 422)


def test_too_many_nameservers(isola_client: Isola) -> None:
    """Creating a sandbox with more than 3 nameservers should be rejected.

    The api-gateway enforces maxItems:3 on the nameservers field via Huma validation.
    """
    with pytest.raises((ValidationError, BadRequestError)) as exc_info:
        isola_client.sandboxes.create(
            image="alpine:3.21",
            network=NetworkSpec(nameservers=["1.1.1.1", "8.8.8.8", "9.9.9.9", "4.4.4.4"]),
        )

    assert exc_info.value.status_code in (400, 422)
