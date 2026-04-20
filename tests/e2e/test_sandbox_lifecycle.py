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

from isola import Isola, Network, Sandbox, SandboxStatus, SandboxSummary, SnapshotRootfs

from utils import parse_k8s_quantity, wait_for_running


@pytest.mark.timeout(90)
def test_create_minimal_sandbox_reaches_running(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Creating a sandbox with only an image should eventually reach running status."""
    sb = sandbox_factory(image="alpine:3.21")
    assert sb.id
    assert sb.status in (SandboxStatus.PENDING, SandboxStatus.RUNNING)

    running = wait_for_running(isola_client, sb.id)
    assert running.status == SandboxStatus.RUNNING


@pytest.mark.timeout(90)
def test_create_sandbox_with_full_config(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Creating a sandbox with all configuration options should succeed."""
    sb = sandbox_factory(
        image="alpine:3.21",
        command=["sleep", "infinity"],
        env={"TEST_VAR": "test_value", "ANOTHER": "123"},
        cpu=0.1,
        memory=128,
        ephemeral_storage=512,
        timeout_seconds=300,
        network=Network(allow_internet_egress=True),
    )
    assert sb.id
    assert sb.timeout_seconds == 300

    running = wait_for_running(isola_client, sb.id)
    assert running.status == SandboxStatus.RUNNING
    assert running.timeout_seconds == 300
    assert running.network is not None
    assert running.network.allow_internet_egress is True


def test_get_sandbox_returns_correct_fields(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """Getting a sandbox by ID returns all expected fields."""
    sb = isola_client.sandboxes.get(session_sandbox.id)

    assert sb.id == session_sandbox.id
    assert sb.status == SandboxStatus.RUNNING
    assert sb.creation_timestamp is not None
    # The response includes the pod template with container image info
    assert sb._data.pod_template is not None
    assert sb._data.pod_template.containers[0] is not None
    assert sb._data.pod_template.containers[0].image == "alpine:3.21"


def test_list_sandboxes_includes_created_sandbox(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """Listing sandboxes includes the shared sandbox."""
    summaries = isola_client.sandboxes.list()
    assert isinstance(summaries, list)
    assert len(summaries) > 0

    sandbox_ids = [s.id for s in summaries]
    assert session_sandbox.id in sandbox_ids

    matching = next(s for s in summaries if s.id == session_sandbox.id)
    assert isinstance(matching, SandboxSummary)
    assert matching.status == SandboxStatus.RUNNING
    assert matching.creation_timestamp is not None


@pytest.mark.timeout(90)
def test_env_vars_are_write_only(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Env vars are accepted on create but not returned in get responses.

    The response model uses ContainerInfo (not Container), which has no env field.
    This is intentional to avoid leaking secrets.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        env={"SECRET_KEY": "super_secret_value", "API_TOKEN": "abc123"},
    )
    wait_for_running(isola_client, sb.id)

    fetched = isola_client.sandboxes.get(sb.id)

    # ContainerInfo does not have an env attribute -- verify it is absent
    container = fetched._data.pod_template.containers[0]
    assert not hasattr(container, "env"), (
        "ContainerInfo should not expose env vars in the response"
    )


@pytest.mark.timeout(90)
def test_default_command_keeps_sandbox_alive(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """When no command is specified, the operator injects 'sleep infinity'.

    The sandbox should stay running and accept commands.
    """
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.id)
    assert running.status == SandboxStatus.RUNNING

    # Prove the sandbox is alive by executing a command inside it
    result = running.commands.run("echo", "hello")
    assert result.exit_code == 0


@pytest.mark.timeout(90)
def test_custom_command_sandbox(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Creating a sandbox with a custom command should work.

    The sandbox should reach running status with the specified command.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        command=["sh", "-c", "sleep 10"],
    )
    running = wait_for_running(isola_client, sb.id)
    assert running.status == SandboxStatus.RUNNING

    # Verify the command was set on the container
    container = running._data.pod_template.containers[0]
    assert container.command is not None
    assert "sh" in container.command


@pytest.mark.timeout(90)
def test_resource_limits_round_trip(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Resource limits set at creation should appear in the GET response."""
    sb = sandbox_factory(
        image="alpine:3.21",
        cpu=0.25,
        memory=256,
        ephemeral_storage=1024,
    )
    running = wait_for_running(isola_client, sb.id)

    container = running._data.pod_template.containers[0]
    assert container.resources is not None, "Expected resources in response"
    assert container.resources.limits is not None, "Expected limits in response"

    # K8s may normalize quantity strings differently across versions,
    # so compare parsed numeric values instead of raw strings.
    assert parse_k8s_quantity(container.resources.limits.cpu) == parse_k8s_quantity("250m")
    assert parse_k8s_quantity(container.resources.limits.memory) == parse_k8s_quantity("256Mi")
    assert parse_k8s_quantity(container.resources.limits.ephemeral_storage) == parse_k8s_quantity("1024Mi")


@pytest.mark.timeout(120)
def test_memory_limit_is_enforced(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A process allocating far more memory than the sandbox limit must be killed."""
    sb = sandbox_factory(image="alpine:3.21", memory=128, timeout_seconds=300)
    running = wait_for_running(isola_client, sb.id)

    # busybox awk: each associative-array entry costs ~80 bytes (key + value +
    # hash overhead), so 10M entries demand ~800 MiB — ~6x the 128 MiB limit.
    result = running.commands.run(
        "sh", "-c",
        'awk \'BEGIN{ for(i=0;i<10000000;i++) a[i]="xxxxxxxxxx"; print "ok" }\'',
        timeout_seconds=60,
    )
    assert result.exit_code != 0, (
        f"allocating ~800 MiB with a 128 MiB limit should be killed, got exit={result.exit_code}"
    )
    assert "ok" not in result.stdout


def test_server_defaults_are_present(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """Server-defaulted fields must always be present in responses.

    The SDK types these as required (non-Optional). If the server stops
    returning defaults, this test and the SDK's Pydantic validation will
    both fail, preventing a silent contract break.
    """
    sb = isola_client.sandboxes.get(session_sandbox.id)
    assert sb.startup_timeout_seconds == 90
    assert sb.containers[0].name == "sandbox0"


@pytest.mark.timeout(30)
def test_termination_policy_snapshot_name_defaults_to_sandbox_id(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """snapshotName in SnapshotRootfs termination policy defaults to the sandbox ID.

    The server resolves this before writing the CRD (kubebuilder can't express
    cross-field defaults), so both create and get responses should reflect it.
    """
    sb = sandbox_factory(
        termination_policy=SnapshotRootfs(),
        max_wait_seconds=0,
    )
    assert sb._data.termination_policy is not None
    assert sb._data.termination_policy.snapshot_rootfs is not None
    assert sb._data.termination_policy.snapshot_rootfs.snapshot_name == sb.id

    fetched = isola_client.sandboxes.get(sb.id)
    assert fetched._data.termination_policy.snapshot_rootfs.snapshot_name == sb.id


def test_list_status_matches_get_status(
    isola_client: Isola,
    session_sandbox: Sandbox,
) -> None:
    """The status returned by list() must match the status returned by get() for the same sandbox.

    Ensures both code paths (list summary vs full get) derive status from the same
    K8s conditions and produce consistent results.
    """
    summaries = isola_client.sandboxes.list()
    summary = next((s for s in summaries if s.id == session_sandbox.id), None)
    assert summary is not None, f"Session sandbox {session_sandbox.id} not found in list"

    details = isola_client.sandboxes.get(session_sandbox.id)

    assert summary.status == details.status
