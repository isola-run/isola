"""Sandbox builder and runtime session.

This module provides a fluent builder API for sandbox configuration
and a context manager for sandbox lifecycle management.
"""

from __future__ import annotations

import os
import uuid
from pathlib import Path
from typing import TYPE_CHECKING, Any, Callable, TypeVar

from .client import IsolaClient
from .exceptions import SandboxCreationError, SandboxNotRunningError
from .types import (
    AttachedVolume,
    ExecResult,
    FileDownloadResult,
    FileUploadResult,
    SandboxConfig,
    SandboxState,
)
from .types import Sandbox as SandboxInfo

if TYPE_CHECKING:
    from .decorators import RemoteFunction

T = TypeVar("T")


class SandboxSession:
    """An active sandbox session with file and execution operations.

    This class represents a running sandbox and provides methods for
    interacting with it. Use as a context manager to ensure cleanup.

    Example:
        with sandbox.run() as session:
            session.upload("data.csv", "/home/user/data.csv")
            result = session.exec("python process.py")
            session.download("/home/user/output.json", "output.json")
    """

    def __init__(
        self,
        client: IsolaClient,
        sandbox_info: SandboxInfo,
        auto_terminate: bool = True,
    ):
        self._client = client
        self._info = sandbox_info
        self._auto_terminate = auto_terminate

    @property
    def id(self) -> str:
        """The sandbox UUID."""
        return self._info.id

    @property
    def name(self) -> str:
        """The sandbox name."""
        return self._info.name

    @property
    def state(self) -> SandboxState:
        """Current sandbox state (may be stale, call refresh() to update)."""
        return self._info.state

    @property
    def info(self) -> SandboxInfo:
        """Full sandbox information."""
        return self._info

    def refresh(self) -> SandboxInfo:
        """Refresh sandbox info from the API.

        Returns:
            Updated SandboxInfo.
        """
        self._info = self._client.get_sandbox(self.id)
        return self._info

    def _ensure_running(self) -> None:
        """Ensure the sandbox is in running state."""
        if self._info.state != SandboxState.RUNNING:
            self.refresh()
            if self._info.state != SandboxState.RUNNING:
                raise SandboxNotRunningError(
                    f"Sandbox is not running (state: {self._info.state.value})",
                    sandbox_id=self.id,
                )

    def exec(
        self,
        command: str,
        *,
        check: bool = False,
        timeout: float | None = None,
    ) -> ExecResult:
        """Execute a shell command in the sandbox.

        Args:
            command: Shell command to execute.
            check: If True, raise ExecutionError on non-zero exit code.
            timeout: Command timeout in seconds.

        Returns:
            ExecResult with stdout, stderr, and exit code.

        Raises:
            SandboxNotRunningError: If sandbox is not running.
            ExecutionError: If check=True and command fails.
        """
        from .exceptions import ExecutionError

        self._ensure_running()
        result = self._client.execute_command(self.id, command, timeout=timeout)

        if check and not result.success:
            raise ExecutionError(
                f"Command failed: {command}",
                exit_code=result.exit_code,
                stdout=result.stdout,
                stderr=result.stderr,
            )

        return result

    def upload(
        self,
        local_path: str | Path,
        remote_path: str,
    ) -> FileUploadResult:
        """Upload a file to the sandbox.

        Args:
            local_path: Path to the local file.
            remote_path: Destination path in the sandbox.

        Returns:
            FileUploadResult with upload status.

        Raises:
            SandboxNotRunningError: If sandbox is not running.
            FileOperationError: If upload fails.
        """
        self._ensure_running()
        return self._client.upload_file(self.id, local_path, remote_path)

    def upload_bytes(self, content: bytes, remote_path: str) -> FileUploadResult:
        """Upload bytes content to the sandbox.

        Args:
            content: Bytes to upload.
            remote_path: Destination path in the sandbox.

        Returns:
            FileUploadResult with upload status.
        """
        self._ensure_running()
        return self._client.upload_bytes(self.id, content, remote_path)

    def upload_text(
        self,
        content: str,
        remote_path: str,
        encoding: str = "utf-8",
    ) -> FileUploadResult:
        """Upload text content to the sandbox.

        Args:
            content: Text to upload.
            remote_path: Destination path in the sandbox.
            encoding: Text encoding.

        Returns:
            FileUploadResult with upload status.
        """
        return self.upload_bytes(content.encode(encoding), remote_path)

    def download(
        self,
        remote_path: str,
        local_path: str | Path | None = None,
    ) -> FileDownloadResult:
        """Download a file from the sandbox.

        Args:
            remote_path: Path to the file in the sandbox.
            local_path: Optional local path to save the file.

        Returns:
            FileDownloadResult with file content.

        Raises:
            SandboxNotRunningError: If sandbox is not running.
            FileOperationError: If download fails.
        """
        self._ensure_running()
        return self._client.download_file(self.id, remote_path, local_path)

    def download_bytes(self, remote_path: str) -> bytes:
        """Download file content as bytes.

        Args:
            remote_path: Path to the file in the sandbox.

        Returns:
            File content as bytes.
        """
        self._ensure_running()
        return self._client.download_bytes(self.id, remote_path)

    def download_text(self, remote_path: str, encoding: str = "utf-8") -> str:
        """Download file content as text.

        Args:
            remote_path: Path to the file in the sandbox.
            encoding: Text encoding.

        Returns:
            File content as string.
        """
        self._ensure_running()
        return self._client.download_text(self.id, remote_path, encoding)

    def terminate(self, force: bool = False) -> None:
        """Terminate the sandbox.

        Args:
            force: Force immediate termination without graceful shutdown.
        """
        self._client.terminate_sandbox(self.id, force=force)

    def __enter__(self) -> SandboxSession:
        return self

    def __exit__(self, *args: Any) -> None:
        if self._auto_terminate:
            try:
                self.terminate()
            except Exception:
                pass  # Best effort cleanup


class Sandbox:
    """Fluent builder for sandbox configuration.

    Example:
        sandbox = (
            Sandbox("my-sandbox")
            .image("python:3.11-slim")
            .cpu(2)
            .memory(4)
            .env({"DEBUG": "true"})
        )

        # Use as context manager
        with sandbox.run() as session:
            session.exec("python script.py")

        # Or use with decorator
        @sandbox.function()
        def process(data):
            return data * 2

        result = process.remote([1, 2, 3])
    """

    def __init__(
        self,
        name: str | None = None,
        *,
        api_key: str | None = None,
        base_url: str | None = None,
    ):
        """Create a new sandbox builder.

        Args:
            name: Human-readable name for the sandbox. If not provided,
                a random name will be generated.
            api_key: API key for authentication. If not provided, reads from
                ISOLA_API_KEY environment variable.
            base_url: Base URL of the Isola Gateway.
        """
        self._name = name or f"sandbox-{uuid.uuid4().hex[:8]}"
        self._api_key = api_key
        self._base_url = base_url
        self._image: str | None = None
        self._region: str | None = None
        self._cpu: float | None = None
        self._memory: float | None = None
        self._disk: float | None = None
        self._gpu: int | None = None
        self._env: dict[str, str] = {}
        self._labels: dict[str, str] = {}
        self._volumes: list[AttachedVolume] = []
        self._auto_start: bool = True

    def image(self, image: str) -> Sandbox:
        """Set the container image.

        Args:
            image: Container image name (e.g., "python:3.11-slim").

        Returns:
            Self for chaining.
        """
        self._image = image
        return self

    def region(self, region: str) -> Sandbox:
        """Set the deployment region.

        Args:
            region: Region identifier.

        Returns:
            Self for chaining.
        """
        self._region = region
        return self

    def cpu(self, cores: float) -> Sandbox:
        """Set CPU allocation.

        Args:
            cores: Number of CPU cores (e.g., 0.5, 1, 2).

        Returns:
            Self for chaining.
        """
        self._cpu = cores
        return self

    def memory(self, gb: float) -> Sandbox:
        """Set memory allocation.

        Args:
            gb: Memory in gigabytes.

        Returns:
            Self for chaining.
        """
        self._memory = gb
        return self

    def disk(self, gb: float) -> Sandbox:
        """Set disk space allocation.

        Args:
            gb: Disk space in gigabytes.

        Returns:
            Self for chaining.
        """
        self._disk = gb
        return self

    def gpu(self, count: int) -> Sandbox:
        """Set GPU allocation.

        Args:
            count: Number of GPUs.

        Returns:
            Self for chaining.
        """
        self._gpu = count
        return self

    def env(self, variables: dict[str, str]) -> Sandbox:
        """Set environment variables.

        Args:
            variables: Dictionary of environment variables.

        Returns:
            Self for chaining.
        """
        self._env.update(variables)
        return self

    def add_env(self, key: str, value: str) -> Sandbox:
        """Add a single environment variable.

        Args:
            key: Variable name.
            value: Variable value.

        Returns:
            Self for chaining.
        """
        self._env[key] = value
        return self

    def labels(self, labels: dict[str, str]) -> Sandbox:
        """Set labels.

        Args:
            labels: Dictionary of labels.

        Returns:
            Self for chaining.
        """
        self._labels.update(labels)
        return self

    def add_label(self, key: str, value: str) -> Sandbox:
        """Add a single label.

        Args:
            key: Label name.
            value: Label value.

        Returns:
            Self for chaining.
        """
        self._labels[key] = value
        return self

    def volume(self, volume_id: str, mount_path: str) -> Sandbox:
        """Attach a volume.

        Args:
            volume_id: ID of the volume to attach.
            mount_path: Path where the volume should be mounted.

        Returns:
            Self for chaining.
        """
        self._volumes.append(AttachedVolume(volume_id=volume_id, mount_path=mount_path))
        return self

    def auto_start(self, enabled: bool = True) -> Sandbox:
        """Configure auto-start behavior.

        Args:
            enabled: Whether to start the sandbox automatically after creation.

        Returns:
            Self for chaining.
        """
        self._auto_start = enabled
        return self

    def _build_config(self) -> SandboxConfig:
        """Build the sandbox configuration."""
        return SandboxConfig(
            name=self._name,
            image=self._image,
            region=self._region,
            cpu=self._cpu,
            memory=self._memory,
            disk=self._disk,
            gpu=self._gpu,
            env=self._env.copy(),
            labels=self._labels.copy(),
            volumes=self._volumes.copy(),
            auto_start=self._auto_start,
        )

    def _get_client(self) -> IsolaClient:
        """Get or create the HTTP client."""
        return IsolaClient(
            api_key=self._api_key,
            base_url=self._base_url,
        )

    def run(
        self,
        *,
        wait: bool = True,
        timeout: float = 120.0,
        auto_terminate: bool = True,
    ) -> SandboxSession:
        """Create and start the sandbox, returning a session.

        This is a context manager that automatically terminates the sandbox
        on exit (unless auto_terminate=False).

        Args:
            wait: If True, wait for sandbox to reach running state.
            timeout: Maximum time to wait for sandbox to start.
            auto_terminate: If True, terminate sandbox when exiting context.

        Returns:
            SandboxSession for interacting with the sandbox.

        Raises:
            SandboxCreationError: If sandbox creation fails.
            TimeoutError_: If waiting for sandbox times out.

        Example:
            with sandbox.run() as session:
                session.exec("echo hello")
        """
        client = self._get_client()
        config = self._build_config()

        try:
            sandbox_info = client.create_sandbox(config)
        except Exception as e:
            raise SandboxCreationError(f"Failed to create sandbox: {e}") from e

        if wait:
            sandbox_info = client.wait_for_sandbox(
                sandbox_info.id,
                target_state=SandboxState.RUNNING,
                timeout=timeout,
            )

        return SandboxSession(
            client=client,
            sandbox_info=sandbox_info,
            auto_terminate=auto_terminate,
        )

    def create(
        self,
        *,
        wait: bool = True,
        timeout: float = 120.0,
    ) -> SandboxSession:
        """Create and start the sandbox without auto-termination.

        Use this when you want to manage the sandbox lifecycle manually.

        Args:
            wait: If True, wait for sandbox to reach running state.
            timeout: Maximum time to wait for sandbox to start.

        Returns:
            SandboxSession for interacting with the sandbox.

        Example:
            session = sandbox.create()
            try:
                session.exec("python script.py")
            finally:
                session.terminate()
        """
        return self.run(wait=wait, timeout=timeout, auto_terminate=False)

    def function(
        self,
        *,
        setup_commands: list[str] | None = None,
    ) -> Callable[[Callable[..., T]], RemoteFunction[T]]:
        """Decorator to mark a function for remote execution.

        The decorated function will be serialized and executed in a sandbox
        when called with .remote().

        Args:
            setup_commands: Optional commands to run before the function
                (e.g., ["pip install numpy"]).

        Returns:
            A decorator that creates a RemoteFunction.

        Example:
            @sandbox.function(setup_commands=["pip install pandas"])
            def analyze(data):
                import pandas as pd
                return pd.DataFrame(data).describe()

            result = analyze.remote([[1, 2], [3, 4]])
        """
        from .decorators import create_function_decorator

        return create_function_decorator(self, setup_commands=setup_commands)

    def command(
        self,
        cmd: str,
        *,
        setup_commands: list[str] | None = None,
    ) -> Callable[[Callable[..., Any]], RemoteFunction[ExecResult]]:
        """Decorator to wrap a shell command for remote execution.

        Args:
            cmd: Shell command to execute. Can include environment variable
                references like $VAR.
            setup_commands: Optional commands to run before the main command.

        Returns:
            A decorator that creates a RemoteFunction returning ExecResult.

        Example:
            @sandbox.command("python process.py --input $INPUT_FILE")
            def process(): pass

            result = process.remote(env={"INPUT_FILE": "/data/input.csv"})
        """
        from .decorators import create_command_decorator

        return create_command_decorator(self, cmd, setup_commands=setup_commands)

    def __repr__(self) -> str:
        parts = [f"Sandbox({self._name!r}"]
        if self._image:
            parts.append(f", image={self._image!r}")
        if self._cpu:
            parts.append(f", cpu={self._cpu}")
        if self._memory:
            parts.append(f", memory={self._memory}")
        parts.append(")")
        return "".join(parts)


# Convenience function for quick sandbox creation
def create_sandbox(
    name: str | None = None,
    *,
    image: str | None = None,
    cpu: float | None = None,
    memory: float | None = None,
    env: dict[str, str] | None = None,
    api_key: str | None = None,
    base_url: str | None = None,
    wait: bool = True,
    timeout: float = 120.0,
    auto_terminate: bool = True,
) -> SandboxSession:
    """Create a sandbox with minimal configuration.

    This is a convenience function for quick sandbox creation without
    using the builder pattern.

    Args:
        name: Sandbox name.
        image: Container image.
        cpu: CPU cores.
        memory: Memory in GB.
        env: Environment variables.
        api_key: API key.
        base_url: Gateway URL.
        wait: Wait for sandbox to start.
        timeout: Start timeout.
        auto_terminate: Auto-terminate on context exit.

    Returns:
        SandboxSession.

    Example:
        with create_sandbox(image="python:3.11") as sbx:
            sbx.exec("python --version")
    """
    builder = Sandbox(name, api_key=api_key, base_url=base_url)
    if image:
        builder.image(image)
    if cpu:
        builder.cpu(cpu)
    if memory:
        builder.memory(memory)
    if env:
        builder.env(env)
    return builder.run(wait=wait, timeout=timeout, auto_terminate=auto_terminate)
