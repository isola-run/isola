from __future__ import annotations

import time

import pytest

from isola import Isola, NetworkSpec, Sandbox

from conftest import wait_for_exit, wait_for_running


def _run_and_collect_stdout(sandbox: Sandbox, *, cmd: str, args: list[str] | None = None, timeout: int | None = None, wait_timeout: float = 30) -> tuple[int, str]:
    """Run a command in a sandbox, wait for it to finish, and return (exit_code, stdout)."""
    command = sandbox.commands.run(cmd=cmd, args=args, timeout=timeout)
    exit_code = wait_for_exit(command, timeout=wait_timeout)
    with command.stdout() as stream:
        output = "".join(chunk for chunk in stream)
    return exit_code, output


@pytest.mark.timeout(90)
def test_default_no_internet(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with no network spec has deny-all egress; outbound connections fail."""
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.id)

    exit_code, _ = _run_and_collect_stdout(
        running,
        cmd="wget",
        args=["-q", "-O-", "--timeout=3", "http://1.1.1.1"],
        timeout=5,
        wait_timeout=15,
    )

    assert exit_code != 0, "wget should fail when network is blocked by default"


@pytest.mark.timeout(90)
def test_internet_egress_enabled(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with allow_internet_egress=True can reach the public internet."""
    sb = sandbox_factory(
        image="alpine:3.21",
        network=NetworkSpec(allow_internet_egress=True, nameservers=["8.8.8.8", "1.1.1.1"]),
    )
    running = wait_for_running(isola_client, sb.id)

    exit_code, output = _run_and_collect_stdout(
        running,
        cmd="wget",
        args=["-q", "-O-", "--timeout=5", "http://1.1.1.1"],
        timeout=10,
        wait_timeout=20,
    )

    assert exit_code == 0, f"wget should succeed with internet egress enabled, got exit code {exit_code}"
    assert len(output) > 0, "Expected some content from 1.1.1.1"


@pytest.mark.timeout(90)
def test_custom_nameservers(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """Custom nameservers are written into /etc/resolv.conf inside the sandbox."""
    sb = sandbox_factory(
        image="alpine:3.21",
        network=NetworkSpec(
            allow_internet_egress=True,
            nameservers=["8.8.8.8"],
        ),
    )
    running = wait_for_running(isola_client, sb.id)

    exit_code, output = _run_and_collect_stdout(
        running,
        cmd="cat",
        args=["/etc/resolv.conf"],
    )

    assert exit_code == 0
    assert "8.8.8.8" in output, f"Expected nameserver 8.8.8.8 in resolv.conf, got: {output}"


@pytest.mark.timeout(90)
def test_allowed_egress_cidrs(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with specific allowed_egress_cidrs can reach the allowed IP."""
    sb = sandbox_factory(
        image="alpine:3.21",
        network=NetworkSpec(
            allowed_egress_cidrs=["1.1.1.1/32"],
            nameservers=["1.1.1.1"],
        ),
    )
    running = wait_for_running(isola_client, sb.id)

    # Try to connect to the allowed CIDR (1.1.1.1 is Cloudflare DNS, responds on HTTP).
    # Use --tries=3 because the per-sandbox NetworkPolicy may take a moment to propagate.
    exit_code, output = _run_and_collect_stdout(
        running,
        cmd="wget",
        args=["-q", "-O-", "--timeout=5", "--tries=3", "http://1.1.1.1"],
        timeout=20,
        wait_timeout=30,
    )

    # With the CIDR allowlisted, we expect wget to at least reach the server.
    # Cloudflare returns a redirect or page, so exit code 0 is expected.
    assert exit_code == 0, (
        f"wget to 1.1.1.1 should succeed with allowed_egress_cidrs=['1.1.1.1/32'], "
        f"got exit code {exit_code}"
    )


@pytest.mark.timeout(90)
def test_dns_sink_default(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """A sandbox with no network spec uses the DNS sink (127.0.0.1) so DNS queries fail fast."""
    sb = sandbox_factory(image="alpine:3.21")
    running = wait_for_running(isola_client, sb.id)

    exit_code, output = _run_and_collect_stdout(
        running,
        cmd="cat",
        args=["/etc/resolv.conf"],
    )

    assert exit_code == 0
    assert "127.0.0.1" in output, (
        f"Expected DNS sink nameserver 127.0.0.1 in resolv.conf, got: {output}"
    )


@pytest.mark.timeout(90)
def test_network_spec_reflected_in_get(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """The network spec sent at creation time is reflected when fetching the sandbox."""
    sb = sandbox_factory(
        image="alpine:3.21",
        network=NetworkSpec(
            allow_internet_egress=True,
            nameservers=["8.8.8.8", "1.1.1.1"],
        ),
    )
    wait_for_running(isola_client, sb.id)

    fetched = isola_client.sandboxes.get(sb.id)

    assert fetched.network is not None, "Expected network spec to be present on fetched sandbox"
    assert fetched.network.allow_internet_egress is True
    assert fetched.network.nameservers is not None
    assert "8.8.8.8" in fetched.network.nameservers
    assert "1.1.1.1" in fetched.network.nameservers


@pytest.mark.timeout(90)
def test_allow_cluster_dns(
    isola_client: Isola,
    sandbox_factory,
) -> None:
    """allowClusterDNS=true switches DNS policy to ClusterFirst; resolv.conf should NOT have the 127.0.0.1 sink."""
    sb = sandbox_factory(
        image="alpine:3.21",
        network=NetworkSpec(allow_cluster_dns=True),
    )
    running = wait_for_running(isola_client, sb.id)

    exit_code, output = _run_and_collect_stdout(
        running,
        cmd="cat",
        args=["/etc/resolv.conf"],
    )

    assert exit_code == 0
    # With ClusterFirst, the resolv.conf should have the kube-dns service IP, not the sink
    assert "127.0.0.1" not in output, (
        f"Expected cluster DNS (not sink 127.0.0.1) in resolv.conf, got: {output}"
    )
