# services/isola_controller/kubernetes_manager.py

from __future__ import annotations

import asyncio
import logging
from typing import Dict, Optional, List
from datetime import datetime

from kubernetes import client, config
from kubernetes import watch as k8s_watch  # type: ignore[attr-defined]
from kubernetes.client.exceptions import ApiException

from common.models.sandbox import SandboxState

logger = logging.getLogger(__name__)


class KubernetesManager:
    """Manages Kubernetes resources for sandboxes"""
    
    def __init__(
        self,
        namespace: str = "isola-sandboxes",
        runtime_class_name: Optional[str] = "gvisor",
        api_server_url: Optional[str] = None,
    ):
        self.namespace = namespace
        self.runtime_class_name = runtime_class_name
        self.api_server_url = api_server_url
        self.core_v1: Optional[client.CoreV1Api] = None
        self.apps_v1: Optional[client.AppsV1Api] = None
        self._initialized = False
        self._init_lock: Optional[asyncio.Lock] = None

    def _get_core_v1(self) -> client.CoreV1Api:
        if self.core_v1 is None:
            raise RuntimeError("KubernetesManager is not initialized (CoreV1Api missing)")
        return self.core_v1

    def _get_apps_v1(self) -> client.AppsV1Api:
        if self.apps_v1 is None:
            raise RuntimeError("KubernetesManager is not initialized (AppsV1Api missing)")
        return self.apps_v1
        
    async def initialize(self) -> None:
        """Initialize Kubernetes client - call from async context"""
        if self._initialized:
            return

        logger.info(
            "Initializing KubernetesManager for namespace '%s' (runtime_class=%s)",
            self.namespace,
            self.runtime_class_name,
        )

        if self._init_lock is None:
            self._init_lock = asyncio.Lock()

        async with self._init_lock:
            if self._initialized:
                return
            
            try:
                config.load_incluster_config()
                logger.info("Loaded in-cluster Kubernetes config")
            except config.ConfigException:
                config.load_kube_config()
                logger.info("Loaded kubeconfig from file")
            api_client: client.ApiClient = client.ApiClient()
            
            self.core_v1 = client.CoreV1Api(api_client)
            self.apps_v1 = client.AppsV1Api(api_client)
            
            self._initialized = True
        

    def _build_pod_spec(
        self,
        sandbox_id: str,
        name: str,
        image: str,
        cpu: float,
        memory: float,
        disk: float,
        env: Dict[str, str],
        labels: Dict[str, str],
        gpu: int = 0
    ) -> client.V1Pod:
        """Build Kubernetes Pod specification for a sandbox"""
        
        # Combine default labels with user labels
        all_labels = {
            "app": "isola-sandbox",
            "sandbox-id": sandbox_id,
            "managed-by": "isola-controller",
            **labels
        }
        
        # Build environment variables
        env_vars = [
            client.V1EnvVar(name=k, value=v) for k, v in env.items()
        ]
        
        # Add sandbox metadata as env vars
        env_vars.extend([
            client.V1EnvVar(name="SANDBOX_ID", value=sandbox_id),
            client.V1EnvVar(name="SANDBOX_NAME", value=name),
        ])
        
        # Build resource requirements
        resource_requests = {
            "cpu": f"{cpu}",
            "memory": f"{int(memory * 1024)}Mi",
            "ephemeral-storage": f"{int(disk)}Gi"
        }
        resource_limits = {
            "cpu": f"{cpu * 2}",  # Allow bursting up to 2x
            "memory": f"{int(memory * 1024)}Mi",
            "ephemeral-storage": f"{int(disk)}Gi"
        }
        
        # Add GPU if requested
        if gpu > 0:
            resource_requests["nvidia.com/gpu"] = str(gpu)
            resource_limits["nvidia.com/gpu"] = str(gpu)

        resource_requirements = client.V1ResourceRequirements(
            requests=resource_requests,
            limits=resource_limits
        )
        
        # Build container spec
        container = client.V1Container(
            name="sandbox",
            image=image,
            env=env_vars,
            resources=resource_requirements,
            # Keep container running
            command=["/bin/sh"],
            args=["-c", "trap 'exit 0' TERM; sleep infinity & wait"],
            # Security context for sandboxing
            security_context=client.V1SecurityContext(
                run_as_non_root=True,
                run_as_user=1000,
                allow_privilege_escalation=False,
                read_only_root_filesystem=False,
                capabilities=client.V1Capabilities(
                    drop=["ALL"]
                )
            )
        )
        
        # Build pod spec
        pod_spec = client.V1PodSpec(
            containers=[container],
            restart_policy="Always",
            runtime_class_name=self.runtime_class_name,
            # Service account for potential RBAC
            # service_account_name="isola-sandbox",
            # DNS policy
            dns_policy="ClusterFirst",
            # Security context for pod
            security_context=client.V1PodSecurityContext(
                fs_group=1000,
                run_as_non_root=True,
                run_as_user=1000
            ),
            # Tolerations for node selection
            tolerations=[
                client.V1Toleration(
                    key="sandbox",
                    operator="Equal",
                    value="true",
                    effect="NoSchedule"
                )
            ]
        )
        
        # Build pod metadata
        metadata = client.V1ObjectMeta(
            name=f"sandbox-{sandbox_id[:8]}",  # Truncate for k8s name length
            namespace=self.namespace,
            labels=all_labels,
            annotations={
                "isola.run/sandbox-id": sandbox_id,
                "isola.run/sandbox-name": name,
                "isola.run/created-at": datetime.utcnow().isoformat()
            }
        )
        
        # Build complete pod
        pod = client.V1Pod(
            api_version="v1",
            kind="Pod",
            metadata=metadata,
            spec=pod_spec
        )
        
        return pod
    
    async def create_pod(
        self,
        sandbox_id: str,
        name: str,
        image: str,
        cpu: float,
        memory: float,
        disk: float,
        env: Dict[str, str],
        labels: Dict[str, str],
        gpu: int = 0,
            auto_start: bool = True
        ) -> tuple[bool, Optional[str], Optional[str]]:
        """
        Create a pod for a sandbox.

        Returns:
            (success, pod_name, error_reason)
        """
        if not self._initialized:
            await self.initialize()

        logger.info(
            "Creating pod for sandbox '%s' (%s) namespace=%s image=%s cpu=%s mem=%sGi disk=%sGi gpu=%s auto_start=%s",
            sandbox_id,
            name,
            self.namespace,
            image,
            cpu,
            memory,
            disk,
            gpu,
            auto_start,
        )
        
        try:
            pod_spec = self._build_pod_spec(
                sandbox_id, name, image, cpu, memory, disk, env, labels, gpu
            )

            core_v1 = self._get_core_v1()
            
            # Create the pod
            response = core_v1.create_namespaced_pod(
                namespace=self.namespace,
                body=pod_spec
            )
            
            pod_name = self._require_metadata_name(response.metadata)
            logger.info(f"Created pod {pod_name} for sandbox {sandbox_id}")
            
            # Wait for pod to be scheduled and get IP
            if auto_start:
                ip_address = await self._wait_for_pod_ip(pod_name, timeout=60)
                return True, ip_address, None
            else:
                # If not auto-starting, stop the pod immediately
                await self.stop_pod(sandbox_id)
                return True, None, None
                
        except ApiException as e:
            logger.error(f"Failed to create pod for sandbox {sandbox_id}: {e}")
            return False, None, f"Kubernetes API error: {e.reason}"
        except Exception as e:
            logger.error(f"Unexpected error creating pod for sandbox {sandbox_id}: {e}")
            return False, None, str(e)
    
    async def _wait_for_pod_ip(self, pod_name: str, timeout: int = 60) -> Optional[str]:
        """Wait for pod to be assigned an IP address"""
        loop = asyncio.get_running_loop()
        start_time = loop.time()
        core_v1 = self._get_core_v1()

        logger.info("Waiting for pod '%s' to receive IP (timeout=%ss)", pod_name, timeout)
        
        while True:
            try:
                pod = core_v1.read_namespaced_pod(
                    name=pod_name,
                    namespace=self.namespace
                )

                status = pod.status
                if status and status.pod_ip:
                    logger.info(f"Pod {pod_name} got IP: {status.pod_ip}")
                    return status.pod_ip
                
                # Check for errors
                if status and status.phase in ["Failed", "Unknown"]:
                    logger.error(f"Pod {pod_name} entered phase: {status.phase}")
                    return None
                
                # Check timeout
                if loop.time() - start_time > timeout:
                    logger.warning(f"Timeout waiting for pod {pod_name} IP")
                    return None
                
                await asyncio.sleep(1)
                
            except ApiException as e:
                logger.error(f"Error reading pod {pod_name}: {e}")
                return None
    
    async def get_pod_status(self, sandbox_id: str) -> tuple[Optional[SandboxState], Optional[str], Optional[str]]:
        """
        Get pod status for a sandbox.
        
        Returns:
            (state, ip_address, error_reason)
        """
        if not self._initialized:
            await self.initialize()

        logger.info("Fetching pod status for sandbox '%s'", sandbox_id)
        
        try:
            core_v1 = self._get_core_v1()
            # Find pod by sandbox ID label
            label_selector = f"sandbox-id={sandbox_id}"
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector
            )
            
            if not pods.items:
                return None, None, "Pod not found"
            
            pod = pods.items[0]
            status = pod.status
            phase = status.phase if status else None
            ip_address = status.pod_ip if status else None
            
            # Map Kubernetes phase to SandboxState
            state_map = {
                "Pending": SandboxState.pending,
                "Running": SandboxState.running,
                "Succeeded": SandboxState.stopped,
                "Failed": SandboxState.error,
                "Unknown": SandboxState.error
            }
            
            state = state_map.get(phase or "", SandboxState.error)
            
            # Get error reason if failed
            error_reason = None
            if (
                status
                and phase == "Failed"
                and status.container_statuses
            ):
                for container_status in status.container_statuses:
                    container_state = container_status.state
                    if container_state and container_state.terminated:
                        error_reason = container_state.terminated.reason
                        break
            
            return state, ip_address, error_reason
            
        except ApiException as e:
            logger.error(f"Failed to get pod status for sandbox {sandbox_id}: {e}")
            return SandboxState.error, None, f"API error: {e.reason}"
    
    async def list_pods(self, label_selector: Optional[str] = None) -> List[Dict]:
        """List all sandbox pods"""
        if not self._initialized:
            await self.initialize()

        logger.info(
            "Listing sandbox pods with selector '%s'",
            label_selector or "app=isola-sandbox",
        )
        
        try:
            core_v1 = self._get_core_v1()
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector or "app=isola-sandbox"
            )
            
            result = []
            for pod in pods.items:
                metadata = pod.metadata
                status = pod.status
                labels = metadata.labels if metadata and metadata.labels else {}
                result.append({
                    "name": metadata.name if metadata else None,
                    "sandbox_id": labels.get("sandbox-id"),
                    "phase": status.phase if status else None,
                    "ip_address": status.pod_ip if status else None,
                    "created_at": (
                        metadata.creation_timestamp.isoformat()
                        if metadata and metadata.creation_timestamp
                        else None
                    )
                })
            
            return result
            
        except ApiException as e:
            logger.error(f"Failed to list pods: {e}")
            return []
    
    async def stop_pod(self, sandbox_id: str) -> tuple[bool, Optional[str]]:
        """
        Stop a pod (scale to 0 replicas by updating with stopped command).
        
        Returns:
            (success, error_reason)
        """
        if not self._initialized:
            await self.initialize()

        logger.info("Stopping pod for sandbox '%s'", sandbox_id)
        
        try:
            core_v1 = self._get_core_v1()
            # Find pod by sandbox ID
            label_selector = f"sandbox-id={sandbox_id}"
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector
            )
            
            if not pods.items:
                logger.info("No pod found to stop for sandbox '%s'", sandbox_id)
                return False, "Pod not found"
            
            pod = pods.items[0]
            pod_name = self._require_metadata_name(pod.metadata)
            
            # Patch pod to use stop command (graceful shutdown)
            # Note: Pods can't be truly "stopped" - we delete with grace period
            # For true stop/start, consider using Deployments with replica scaling
            body = {
                "spec": {
                    "containers": [{
                        "name": "sandbox",
                        "command": ["/bin/sh"],
                        "args": ["-c", "exit 0"]
                    }]
                }
            }
            
            core_v1.patch_namespaced_pod(
                name=pod_name,
                namespace=self.namespace,
                body=body
            )
            
            logger.info(f"Stopped pod {pod_name} for sandbox {sandbox_id}")
            return True, None
            
        except ApiException as e:
            logger.error(f"Failed to stop pod for sandbox {sandbox_id}: {e}")
            return False, f"API error: {e.reason}"
    
    async def terminate_pod(self, sandbox_id: str, force: bool = False) -> tuple[bool, Optional[str]]:
        """
        Terminate a pod for a sandbox.
        
        Args:
            sandbox_id: The sandbox ID
            force: If True, delete immediately without grace period
        
        Returns:
            (success, error_reason)
        """
        if not self._initialized:
            await self.initialize()

        logger.info(
            "Deleting pod for sandbox '%s' (force=%s)",
            sandbox_id,
            force,
        )
        
        try:
            core_v1 = self._get_core_v1()
            # Find pod by sandbox ID
            label_selector = f"sandbox-id={sandbox_id}"
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector
            )
            
            if not pods.items:
                logger.info("No pod found to delete for sandbox '%s'", sandbox_id)
                return False, "Pod not found"
            
            pod = pods.items[0]

            # TODO: __OMER__ do snapshot according to config 
            pod_name = self._require_metadata_name(pod.metadata)
            
            # Delete options
            delete_options = client.V1DeleteOptions(
                grace_period_seconds=0 if force else 30,
                propagation_policy="Foreground"
            )
            
            core_v1.delete_namespaced_pod(
                name=pod_name,
                namespace=self.namespace,
                body=delete_options
            )
            
            logger.info(f"Deleted pod {pod_name} for sandbox {sandbox_id} (force={force})")
            return True, None
            
        except ApiException as e:
            if e.status == 404:
                logger.warning(f"Pod for sandbox {sandbox_id} already deleted")
                return True, None
            logger.error(f"Failed to delete pod for sandbox {sandbox_id}: {e}")
            return False, f"API error: {e.reason}"
    
    async def restart_pod(self, sandbox_id: str) -> tuple[bool, Optional[str], Optional[str]]:
        """
        Restart a pod by deleting and waiting for recreation (if using Deployment).
        For standalone pods, we delete and recreate.
        
        Returns:
            (success, ip_address, error_reason)
        """
        if not self._initialized:
            await self.initialize()

        logger.info("Restarting pod for sandbox '%s'", sandbox_id)
        
        try:
            core_v1 = self._get_core_v1()
            # Get current pod configuration
            label_selector = f"sandbox-id={sandbox_id}"
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector
            )
            
            if not pods.items:
                return False, None, "Pod not found"
            
            # Delete the pod (gracefully)
            success, error = await self.delete_pod(sandbox_id, force=False)
            if not success:
                return False, None, error
            
            # Wait for deletion to complete
            await asyncio.sleep(2)
            
            # Recreate pod with same spec
            # Note: Ideally this should be stored and reused
            # For now, let the controller handle recreation via desired state
            
            logger.info(f"Restarted pod for sandbox {sandbox_id}")
            return True, None, None
            
        except ApiException as e:
            logger.error(f"Failed to restart pod for sandbox {sandbox_id}: {e}")
            return False, None, f"API error: {e.reason}"
    
    async def watch_pod_events(self, sandbox_id: Optional[str] = None, callback=None):
        """
        Watch for pod events and call callback with event data.
        
        Args:
            sandbox_id: Optional sandbox ID to watch specific pod
            callback: Async function to call with (event_type, pod) on each event
        """
        if not self._initialized:
            await self.initialize()
        
        label_selector = "app=isola-sandbox"
        if sandbox_id:
            label_selector = f"sandbox-id={sandbox_id}"
        
        core_v1 = self._get_core_v1()
        w = k8s_watch.Watch()

        logger.info(
            "Watching pod events for selector '%s'%s",
            label_selector,
            f" (sandbox_id={sandbox_id})" if sandbox_id else "",
        )
        
        try:
            for event in w.stream(
                core_v1.list_namespaced_pod,
                namespace=self.namespace,
                label_selector=label_selector,
                timeout_seconds=0  # Watch indefinitely
            ):
                event_type = event['type']  # ADDED, MODIFIED, DELETED
                pod = event['object']
                
                pod_name = pod.metadata.name if pod.metadata else "unknown"
                logger.debug(f"Pod event: {event_type} {pod_name}")
                
                if callback:
                    await callback(event_type, pod)
                    
        except ApiException as e:
            logger.error(f"Error watching pod events: {e}")
        finally:
            w.stop()
            logger.info("Stopped watching pod events for selector '%s'", label_selector)
    
    async def execute_command(self, sandbox_id: str, command: str) -> tuple[str, str, int]:
        """
        Execute a command in a pod.
        
        Args:
            sandbox_id: The sandbox ID (pod label)
            command: The command to execute
            
        Returns:
            (stdout, stderr, exit_code)
        """
        if not self._initialized:
            await self.initialize()
        
        try:
            from kubernetes.stream import stream
            
            core_v1 = self._get_core_v1()
            
            # Find pod by sandbox ID
            label_selector = f"sandbox-id={sandbox_id}"
            pods = core_v1.list_namespaced_pod(
                namespace=self.namespace,
                label_selector=label_selector
            )
            
            if not pods.items:
                logger.error(f"Pod not found for sandbox {sandbox_id}")
                return "", f"Pod not found for sandbox {sandbox_id}", 1
            
            pod = pods.items[0]
            pod_name = self._require_metadata_name(pod.metadata)
            
            # Execute command in pod
            exec_command = ['/bin/sh', '-c', command]
            
            resp = stream(
                core_v1.connect_get_namespaced_pod_exec,
                pod_name,
                self.namespace,
                command=exec_command,
                stderr=True,
                stdin=False,
                stdout=True,
                tty=False,
                _preload_content=False
            )
            
            # Read stdout and stderr
            stdout_data = ""
            stderr_data = ""
            
            while resp.is_open():
                resp.update(timeout=1)
                if resp.peek_stdout():
                    stdout_data += resp.read_stdout()
                if resp.peek_stderr():
                    stderr_data += resp.read_stderr()
            
            # Get exit code
            exit_code = resp.returncode if hasattr(resp, 'returncode') else 0
            
            resp.close()
            
            logger.info(f"Executed command in pod {pod_name}: {command[:50]}...")
            return stdout_data, stderr_data, exit_code
            
        except ApiException as e:
            logger.error(f"Failed to execute command in sandbox {sandbox_id}: {e}")
            return "", f"API error: {e.reason}", 1
        except Exception as e:
            logger.error(f"Unexpected error executing command in sandbox {sandbox_id}: {e}")
            return "", str(e), 1
    
    async def cleanup(self):
        """Cleanup resources"""
        logger.info("Cleanup requested for KubernetesManager (no-op)")
        # Kubernetes client doesn't need explicit cleanup

    @staticmethod
    def _require_metadata_name(metadata: Optional[client.V1ObjectMeta]) -> str:
        if metadata is None or metadata.name is None:
            raise RuntimeError("Kubernetes Pod metadata missing name")
        return metadata.name
