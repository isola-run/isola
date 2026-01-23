"""
Kubernetes helper for direct cluster assertions.
Optional - tests can run without this if kubeconfig is not available.
"""
from __future__ import annotations

import logging
from typing import TYPE_CHECKING, Any

logger = logging.getLogger(__name__)

try:
    from kubernetes import client, config
    from kubernetes.client.rest import ApiException

    K8S_AVAILABLE = True
except ImportError:
    K8S_AVAILABLE = False
    logger.warning("kubernetes package not installed - k8s assertions disabled")

if TYPE_CHECKING:
    from kubernetes.client import V1Pod


class K8sHelper:
    """Helper for direct Kubernetes cluster operations."""

    def __init__(self, kubeconfig: str | None = None):
        if not K8S_AVAILABLE:
            raise ImportError("kubernetes package required")

        if kubeconfig:
            config.load_kube_config(config_file=kubeconfig)
        else:
            try:
                config.load_incluster_config()
            except config.ConfigException:
                config.load_kube_config()

        self.core_v1 = client.CoreV1Api()
        self.custom = client.CustomObjectsApi()

    def get_sandbox_pod(
        self,
        sandbox_id: str,
        namespace: str = "isola-sandboxes",
    ) -> V1Pod | None:
        """Get the pod for a sandbox by its ID."""
        try:
            pods = self.core_v1.list_namespaced_pod(
                namespace=namespace,
                label_selector=f"sandbox-id={sandbox_id}",
            )
            if pods.items:
                return pods.items[0]
            return None
        except ApiException as e:
            logger.error(f"Failed to get sandbox pod: {e}")
            return None

    def get_sandbox_cr(
        self,
        sandbox_id: str,
        namespace: str = "isola-sandboxes",
    ) -> dict | None:
        """Get the Sandbox custom resource."""
        try:
            return self.custom.get_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="sandboxes",
                name=f"sandbox-{sandbox_id}",
            )
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    def pod_is_running(
        self,
        sandbox_id: str,
        namespace: str = "isola-sandboxes",
    ) -> bool:
        """Check if the sandbox pod is running."""
        pod = self.get_sandbox_pod(sandbox_id, namespace)
        if not pod or not pod.status:
            return False
        return pod.status.phase == "Running"

    def get_pod_logs(
        self,
        sandbox_id: str,
        container: str = "sandbox",
        namespace: str = "isola-sandboxes",
        tail_lines: int = 100,
    ) -> str:
        """Get logs from a sandbox pod."""
        pod = self.get_sandbox_pod(sandbox_id, namespace)
        if not pod or not pod.metadata:
            return ""

        try:
            return self.core_v1.read_namespaced_pod_log(
                name=pod.metadata.name,
                namespace=namespace,
                container=container,
                tail_lines=tail_lines,
            )
        except ApiException as e:
            logger.error(f"Failed to get pod logs: {e}")
            return ""

    def list_pods_in_namespace(
        self,
        namespace: str = "isola-sandboxes",
    ) -> list[V1Pod]:
        """List all pods in a namespace."""
        try:
            pods = self.core_v1.list_namespaced_pod(namespace=namespace)
            return pods.items
        except ApiException as e:
            logger.error(f"Failed to list pods: {e}")
            return []

    # =========================================================================
    # RootfsSnapshot operations
    # =========================================================================

    def create_rootfs_snapshot(
        self,
        name: str,
        sandbox_name: str,
        container_names: list[str],
        namespace: str = "isola-sandboxes",
    ) -> dict:
        """Create a RootfsSnapshot custom resource."""
        body = {
            "apiVersion": "sandbox.isola.run/v1alpha1",
            "kind": "RootfsSnapshot",
            "metadata": {
                "name": name,
                "namespace": namespace,
            },
            "spec": {
                "sandboxName": sandbox_name,
                "containerNames": container_names,
            },
        }
        return self.custom.create_namespaced_custom_object(
            group="sandbox.isola.run",
            version="v1alpha1",
            namespace=namespace,
            plural="rootfssnapshots",
            body=body,
        )

    def get_rootfs_snapshot(
        self,
        name: str,
        namespace: str = "isola-sandboxes",
    ) -> dict | None:
        """Get a RootfsSnapshot custom resource."""
        try:
            return self.custom.get_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="rootfssnapshots",
                name=name,
            )
        except ApiException as e:
            if e.status == 404:
                return None
            raise

    def delete_rootfs_snapshot(
        self,
        name: str,
        namespace: str = "isola-sandboxes",
    ) -> None:
        """Delete a RootfsSnapshot custom resource."""
        try:
            self.custom.delete_namespaced_custom_object(
                group="sandbox.isola.run",
                version="v1alpha1",
                namespace=namespace,
                plural="rootfssnapshots",
                name=name,
            )
        except ApiException as e:
            if e.status != 404:
                raise

    def list_rootfs_snapshots(
        self,
        namespace: str = "isola-sandboxes",
        label_selector: str | None = None,
    ) -> list[dict]:
        """List RootfsSnapshot custom resources."""
        kwargs: dict[str, Any] = {
            "group": "sandbox.isola.run",
            "version": "v1alpha1",
            "namespace": namespace,
            "plural": "rootfssnapshots",
        }
        if label_selector:
            kwargs["label_selector"] = label_selector

        result = self.custom.list_namespaced_custom_object(**kwargs)
        return result.get("items", [])

    def wait_for_snapshot_condition(
        self,
        name: str,
        condition_type: str,
        expected_status: str,
        namespace: str = "isola-sandboxes",
        timeout: int = 120,
        poll_interval: float = 2.0,
    ) -> dict:
        """Wait for a RootfsSnapshot to reach a specific condition status."""
        import time

        deadline = time.time() + timeout
        last_reason = None

        while time.time() < deadline:
            snap = self.get_rootfs_snapshot(name, namespace)
            if not snap:
                time.sleep(poll_interval)
                continue

            conditions = snap.get("status", {}).get("conditions", [])
            for cond in conditions:
                if cond.get("type") == condition_type:
                    current_status = cond.get("status")
                    current_reason = cond.get("reason")

                    if current_reason != last_reason:
                        logger.info(
                            f"Snapshot {name}: {condition_type}={current_status} "
                            f"reason={current_reason}"
                        )
                        last_reason = current_reason

                    if current_status == expected_status:
                        return snap

            time.sleep(poll_interval)

        raise TimeoutError(
            f"Snapshot {name} did not reach {condition_type}={expected_status} "
            f"within {timeout}s (last reason: {last_reason})"
        )
