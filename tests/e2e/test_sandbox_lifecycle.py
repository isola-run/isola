from __future__ import annotations

import pytest

from isola import Isola, NetworkSpec, Sandbox, SandboxStatus, SandboxSummary

from conftest import wait_for_running


@pytest.mark.timeout(90)
def test_create_minimal_sandbox_reaches_running(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Creating a sandbox with only an image should eventually reach running status."""
    sb = sandbox_factory(image="alpine:3.21")
    assert sb.id
    assert sb.status in (SandboxStatus.CREATING, SandboxStatus.RUNNING, SandboxStatus.UNKNOWN)

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
        cpu="100m",
        memory="128Mi",
        ephemeral_storage="512Mi",
        timeout=300,
        network=NetworkSpec(allow_internet_egress=True),
    )
    assert sb.id
    assert sb.timeout == 300

    running = wait_for_running(isola_client, sb.id)
    assert running.status == SandboxStatus.RUNNING
    assert running.timeout == 300
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
    assert sb._data.pod_template.container is not None
    assert sb._data.pod_template.container.image == "alpine:3.21"


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

    The response model uses ContainerInfo (not ContainerSpec), which has no env field.
    This is intentional to avoid leaking secrets.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        env={"SECRET_KEY": "super_secret_value", "API_TOKEN": "abc123"},
    )
    wait_for_running(isola_client, sb.id)

    fetched = isola_client.sandboxes.get(sb.id)

    # ContainerInfo does not have an env attribute -- verify it is absent
    container = fetched._data.pod_template.container
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
    container = running._data.pod_template.container
    assert container.command is not None
    assert "sh" in container.command


@pytest.mark.timeout(90)
def test_resource_limits_round_trip(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Resource limits set at creation should appear in the GET response.

    Tests the full quantity conversion: REST string -> K8s resource.Quantity -> REST string.
    """
    sb = sandbox_factory(
        image="alpine:3.21",
        cpu="250m",
        memory="256Mi",
        ephemeral_storage="1Gi",
    )
    running = wait_for_running(isola_client, sb.id)

    container = running._data.pod_template.container
    assert container.resources is not None, "Expected resources in response"
    assert container.resources.limits is not None, "Expected limits in response"

    # K8s may normalize quantities, so check semantic equivalence
    assert container.resources.limits.cpu == "250m"
    assert container.resources.limits.memory == "256Mi"
    assert container.resources.limits.ephemeral_storage == "1Gi"
