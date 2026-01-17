"""
Tests for network connectivity from sandboxes.
These tests verify that network policies are correctly applied and that
sandboxes can reach allowed destinations.
"""
from __future__ import annotations

import logging
import time
import uuid
from typing import TYPE_CHECKING, Generator

import pytest

from client.isola_client import IsolaClient, IsolaError
from helpers.k8s import K8sHelper, K8S_AVAILABLE

if TYPE_CHECKING:
    from _pytest.fixtures import FixtureRequest

logger = logging.getLogger(__name__)

pytestmark = pytest.mark.network


@pytest.fixture(scope="module")
def k8s_helper() -> K8sHelper | None:
    """Create a K8s helper for direct cluster operations."""
    if not K8S_AVAILABLE:
        pytest.skip("kubernetes package not installed")
    try:
        return K8sHelper()
    except Exception as e:
        pytest.skip(f"Failed to connect to cluster: {e}")


@pytest.fixture(scope="module")
def localstack_network_template(k8s_helper: K8sHelper) -> Generator[str, None, None]:
    """
    Create a NetworkTemplate that allows egress to localstack.
    This template allows DNS resolution and HTTP access to localstack.
    """
    template_name = f"localstack-egress-{uuid.uuid4().hex[:6]}"
    namespace = "isola-sandboxes"

    template_body = {
        "apiVersion": "sandbox.isola.run/v1alpha1",
        "kind": "NetworkTemplate",
        "metadata": {
            "name": template_name,
            "namespace": namespace,
        },
        "spec": {
            "dnsPolicy": "ClusterFirst",
            "allowedEgressPods": [
                {
                    "namespace": "kube-system",
                    "podSelector": {
                        "matchLabels": {"k8s-app": "kube-dns"},
                    },
                    "ports": [
                        {"protocol": "UDP", "port": 53},
                        {"protocol": "TCP", "port": 53},
                    ],
                },
                {
                    "namespace": "localstack",
                    "podSelector": {
                        "matchLabels": {"app.kubernetes.io/name": "localstack"},
                    },
                    "ports": [
                        {"protocol": "TCP", "port": 4566},
                    ],
                },
            ],
        },
    }

    logger.info(f"Creating NetworkTemplate {template_name}")
    k8s_helper.custom.create_namespaced_custom_object(
        group="sandbox.isola.run",
        version="v1alpha1",
        namespace=namespace,
        plural="networktemplates",
        body=template_body,
    )

    # Wait for the template to be ready
    for _ in range(30):
        template = k8s_helper.custom.get_namespaced_custom_object(
            group="sandbox.isola.run",
            version="v1alpha1",
            namespace=namespace,
            plural="networktemplates",
            name=template_name,
        )
        conditions = template.get("status", {}).get("conditions", [])
        ready = any(
            c.get("type") == "Ready" and c.get("status") == "True"
            for c in conditions
        )
        if ready:
            logger.info(f"NetworkTemplate {template_name} is ready")
            break
        time.sleep(1)
    else:
        pytest.fail(f"NetworkTemplate {template_name} did not become ready")

    yield template_name

    # Cleanup
    try:
        k8s_helper.custom.delete_namespaced_custom_object(
            group="sandbox.isola.run",
            version="v1alpha1",
            namespace=namespace,
            plural="networktemplates",
            name=template_name,
        )
        logger.info(f"Deleted NetworkTemplate {template_name}")
    except Exception as e:
        logger.warning(f"Failed to delete NetworkTemplate {template_name}: {e}")


@pytest.fixture
def sandbox_with_localstack_access(
    k8s_helper: K8sHelper,
    localstack_network_template: str,
    isola_client: IsolaClient,
    skip_cleanup: bool,
    request: FixtureRequest,
) -> Generator[dict, None, None]:
    """
    Create a sandbox with network access to localstack.
    This creates the Sandbox CR directly with the network template reference.
    """
    namespace = "isola-sandboxes"
    # Use full UUID as the sandbox name (K8s resource name = ID)
    sandbox_id = str(uuid.uuid4())
    sandbox_name = sandbox_id
    template_name = sandbox_id  # SandboxTemplate uses same name

    # Create SandboxTemplate first
    template_body = {
        "apiVersion": "sandbox.isola.run/v1alpha1",
        "kind": "SandboxTemplate",
        "metadata": {
            "name": template_name,
            "namespace": namespace,
        },
        "spec": {
            "podTemplate": {
                "spec": {
                    "containers": [
                        {
                            "name": "sandbox",
                            "image": "python:3.11-slim",
                            "command": ["sleep", "infinity"],
                        }
                    ],
                }
            },
        },
    }

    logger.info(f"Creating SandboxTemplate {template_name}")
    k8s_helper.custom.create_namespaced_custom_object(
        group="sandbox.isola.run",
        version="v1alpha1",
        namespace=namespace,
        plural="sandboxtemplates",
        body=template_body,
    )

    # Create Sandbox with network template reference
    sandbox_body = {
        "apiVersion": "sandbox.isola.run/v1alpha1",
        "kind": "Sandbox",
        "metadata": {
            "name": sandbox_name,
            "namespace": namespace,
            "labels": {
                "app.kubernetes.io/managed-by": "e2e-test",
            },
        },
        "spec": {
            "templateRef": {
                "name": template_name,
            },
            "network": {
                "templateRef": {
                    "name": localstack_network_template,
                },
            },
        },
    }

    logger.info(f"Creating Sandbox {sandbox_name} with network template {localstack_network_template}")
    k8s_helper.custom.create_namespaced_custom_object(
        group="sandbox.isola.run",
        version="v1alpha1",
        namespace=namespace,
        plural="sandboxes",
        body=sandbox_body,
    )

    # Wait for sandbox to be ready
    sandbox_data = {"id": sandbox_id, "name": sandbox_name}
    try:
        for _ in range(90):
            sandbox = k8s_helper.custom.get_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="sandboxes",
                name=sandbox_name,
            )
            conditions = sandbox.get("status", {}).get("conditions", [])
            ready = any(
                c.get("type") == "Ready" and c.get("status") == "True"
                for c in conditions
            )
            if ready:
                logger.info(f"Sandbox {sandbox_name} is ready")
                break
            time.sleep(1)
        else:
            # Get the current conditions for debugging
            sandbox = k8s_helper.custom.get_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="sandboxes",
                name=sandbox_name,
            )
            conditions = sandbox.get("status", {}).get("conditions", [])
            pytest.fail(f"Sandbox {sandbox_name} did not become ready. Conditions: {conditions}")

    except Exception as e:
        # Cleanup on failure
        if not skip_cleanup:
            try:
                k8s_helper.custom.delete_namespaced_custom_object(
                    group="sandbox.isola.run",
                    version="v1alpha1",
                    namespace=namespace,
                    plural="sandboxes",
                    name=sandbox_name,
                )
                k8s_helper.custom.delete_namespaced_custom_object(
                    group="sandbox.isola.run",
                    version="v1alpha1",
                    namespace=namespace,
                    plural="sandboxtemplates",
                    name=template_name,
                )
            except Exception:
                pass
        raise e

    yield sandbox_data

    # Cleanup
    if not skip_cleanup:
        try:
            k8s_helper.custom.delete_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="sandboxes",
                name=sandbox_name,
            )
            logger.info(f"Deleted Sandbox {sandbox_name}")
        except Exception as e:
            logger.warning(f"Failed to delete Sandbox {sandbox_name}: {e}")

        try:
            k8s_helper.custom.delete_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="sandboxtemplates",
                name=template_name,
            )
            logger.info(f"Deleted SandboxTemplate {template_name}")
        except Exception as e:
            logger.warning(f"Failed to delete SandboxTemplate {template_name}: {e}")


class TestLocalstackConnectivity:
    """Test sandbox connectivity to in-cluster localstack."""

    def test_sandbox_can_reach_localstack(
        self,
        sandbox_with_localstack_access: dict,
        isola_client: IsolaClient,
    ) -> None:
        """
        Verify that a sandbox with localstack egress rules can reach localstack.
        This tests the full network policy chain: NetworkTemplate -> NetworkPolicy -> egress allowed.
        """
        sandbox_id = sandbox_with_localstack_access["id"]

        # Test DNS resolution first
        result = isola_client.execute_command(
            sandbox_id,
            "python -c \"import socket; print(socket.gethostbyname('localstack.localstack.svc.cluster.local'))\"",
        )
        assert result["exitCode"] == 0, f"DNS resolution failed: {result.get('stderr', '')}"
        logger.info(f"DNS resolution successful: {result['stdout'].strip()}")

        # Test HTTP connectivity to localstack
        result = isola_client.execute_command(
            sandbox_id,
            "python -c \""
            "import urllib.request; "
            "resp = urllib.request.urlopen('http://localstack.localstack.svc.cluster.local:4566/_localstack/health', timeout=10); "
            "print(resp.status)"
            "\"",
        )
        assert result["exitCode"] == 0, f"HTTP request failed: {result.get('stderr', '')}"
        assert "200" in result["stdout"], f"Unexpected response: {result['stdout']}"
        logger.info("Successfully connected to localstack health endpoint")

    def test_sandbox_cannot_reach_external_without_egress(
        self,
        sandbox_with_localstack_access: dict,
        isola_client: IsolaClient,
    ) -> None:
        """
        Verify that the sandbox cannot reach external IPs not in the egress rules.
        The localstack egress template only allows kube-dns and localstack.
        """
        sandbox_id = sandbox_with_localstack_access["id"]

        # Try to reach an external IP (Google DNS) - should fail/timeout
        result = isola_client.execute_command(
            sandbox_id,
            "python -c \""
            "import urllib.request; "
            "import socket; "
            "socket.setdefaulttimeout(3); "
            "try: "
            "    urllib.request.urlopen('http://8.8.8.8', timeout=3); "
            "    print('CONNECTED'); "
            "except Exception as e: "
            "    print(f'BLOCKED: {type(e).__name__}')"
            "\"",
            timeout=15,
        )

        # The request should either timeout or be blocked
        assert "CONNECTED" not in result["stdout"], \
            "Sandbox should not be able to reach external IPs without egress rules"
        logger.info(f"External access correctly blocked: {result['stdout'].strip()}")


class TestDefaultIsolatedNetwork:
    """Test the default isolated network policy behavior."""

    def test_default_sandbox_is_isolated(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """
        Verify that default sandboxes (using isola-isolated template) cannot reach cluster services.
        The default isolated template uses DNSPolicy: None with sink nameserver.
        """
        sandbox_id = sandbox["id"]

        # DNS resolution should fail (using sink nameserver 127.0.0.1)
        result = isola_client.execute_command(
            sandbox_id,
            "python -c \""
            "import socket; "
            "socket.setdefaulttimeout(2); "
            "try: "
            "    ip = socket.gethostbyname('localstack.localstack.svc.cluster.local'); "
            "    print(f'RESOLVED: {ip}'); "
            "except Exception as e: "
            "    print(f'FAILED: {type(e).__name__}')"
            "\"",
            timeout=10,
        )

        # DNS should fail because sink nameserver doesn't resolve anything
        assert "RESOLVED" not in result["stdout"], \
            "Default sandbox should not be able to resolve cluster DNS"
        logger.info(f"DNS correctly isolated: {result['stdout'].strip()}")
