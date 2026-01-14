"""Decorators for remote function execution.

This module provides decorators that mark functions for execution
in sandboxes, supporting both serialized Python functions and
shell command wrappers.
"""

from __future__ import annotations

import functools
from typing import TYPE_CHECKING, Any, Callable, Generic, TypeVar, overload

from .exceptions import ExecutionError
from .serializer import parse_result, serialize_call
from .types import ExecResult

if TYPE_CHECKING:
    from .sandbox import Sandbox, SandboxSession

T = TypeVar("T")


class RemoteFunction(Generic[T]):
    """A function wrapped for remote execution in a sandbox.

    RemoteFunction wraps a Python function or shell command and provides
    methods for remote execution. The original function is preserved and
    can still be called locally.

    Attributes:
        local: The original function for local execution.
    """

    def __init__(
        self,
        func: Callable[..., T],
        sandbox: Sandbox,
        *,
        setup_commands: list[str] | None = None,
        is_command: bool = False,
        command_template: str | None = None,
    ):
        self._func = func
        self._sandbox = sandbox
        self._setup_commands = setup_commands or []
        self._is_command = is_command
        self._command_template = command_template

        # Preserve function metadata
        functools.update_wrapper(self, func)

    @property
    def local(self) -> Callable[..., T]:
        """Get the original function for local execution."""
        return self._func

    def __call__(self, *args: Any, **kwargs: Any) -> T:
        """Call the function locally.

        For remote execution, use .remote() instead.
        """
        return self._func(*args, **kwargs)

    def remote(
        self,
        *args: Any,
        env: dict[str, str] | None = None,
        timeout: float | None = None,
        **kwargs: Any,
    ) -> T:
        """Execute the function remotely in a sandbox.

        Creates a sandbox, uploads the serialized function, executes it,
        and returns the result. The sandbox is automatically terminated
        after execution.

        Args:
            *args: Positional arguments to pass to the function.
            env: Additional environment variables for this execution.
            timeout: Execution timeout in seconds.
            **kwargs: Keyword arguments to pass to the function.

        Returns:
            The function's return value.

        Raises:
            SandboxCreationError: If sandbox creation fails.
            ExecutionError: If execution fails.
            SerializationError: If serialization/deserialization fails.
        """
        # Merge environment variables
        merged_env = self._sandbox._env.copy()
        if env:
            merged_env.update(env)

        # Create a modified sandbox with merged env
        sandbox = self._sandbox
        if env:
            sandbox = Sandbox(
                sandbox._name,
                api_key=sandbox._api_key,
                base_url=sandbox._base_url,
            )
            sandbox._image = self._sandbox._image
            sandbox._region = self._sandbox._region
            sandbox._cpu = self._sandbox._cpu
            sandbox._memory = self._sandbox._memory
            sandbox._disk = self._sandbox._disk
            sandbox._gpu = self._sandbox._gpu
            sandbox._env = merged_env
            sandbox._labels = self._sandbox._labels.copy()
            sandbox._volumes = self._sandbox._volumes.copy()
            sandbox._auto_start = self._sandbox._auto_start

        with sandbox.run() as session:
            return self._execute_in_session(session, args, kwargs, timeout)

    def remote_in(
        self,
        session: SandboxSession,
        *args: Any,
        timeout: float | None = None,
        **kwargs: Any,
    ) -> T:
        """Execute the function in an existing sandbox session.

        This allows reusing a sandbox for multiple function calls,
        which is more efficient than creating a new sandbox each time.

        Args:
            session: An existing SandboxSession.
            *args: Positional arguments to pass to the function.
            timeout: Execution timeout in seconds.
            **kwargs: Keyword arguments to pass to the function.

        Returns:
            The function's return value.
        """
        return self._execute_in_session(session, args, kwargs, timeout)

    def _execute_in_session(
        self,
        session: SandboxSession,
        args: tuple[Any, ...],
        kwargs: dict[str, Any],
        timeout: float | None,
    ) -> T:
        """Execute the function in a sandbox session."""
        # Run setup commands
        for cmd in self._setup_commands:
            result = session.exec(cmd, timeout=timeout)
            if not result.success:
                raise ExecutionError(
                    f"Setup command failed: {cmd}",
                    exit_code=result.exit_code,
                    stdout=result.stdout,
                    stderr=result.stderr,
                )

        if self._is_command:
            return self._execute_command(session, timeout)  # type: ignore
        else:
            return self._execute_function(session, args, kwargs, timeout)

    def _execute_function(
        self,
        session: SandboxSession,
        args: tuple[Any, ...],
        kwargs: dict[str, Any],
        timeout: float | None,
    ) -> T:
        """Execute a serialized Python function."""
        # Create the runner script
        script = serialize_call(self._func, args, kwargs)

        # Upload the script
        script_path = "/tmp/_isola_runner.py"
        session.upload_text(script, script_path)

        # Execute the script
        result = session.exec(f"python {script_path}", timeout=timeout)

        # Parse and return the result
        return parse_result(result.stdout, result.stderr, result.exit_code)

    def _execute_command(
        self,
        session: SandboxSession,
        timeout: float | None,
    ) -> ExecResult:
        """Execute a shell command."""
        if not self._command_template:
            raise ExecutionError("No command template specified")

        result = session.exec(self._command_template, timeout=timeout)
        return result

    def map(
        self,
        inputs: list[tuple[Any, ...]] | list[Any],
        *,
        timeout: float | None = None,
    ) -> list[T]:
        """Execute the function for multiple inputs.

        Creates a single sandbox and runs the function for each input,
        which is more efficient than calling .remote() multiple times.

        Args:
            inputs: List of input arguments. Each item can be:
                - A single value (passed as first positional arg)
                - A tuple of positional args
            timeout: Per-execution timeout in seconds.

        Returns:
            List of results in the same order as inputs.

        Example:
            @sandbox.function()
            def process(x):
                return x * 2

            results = process.map([1, 2, 3, 4, 5])  # [2, 4, 6, 8, 10]
        """
        results: list[T] = []
        with self._sandbox.run() as session:
            # Run setup commands once
            for cmd in self._setup_commands:
                result = session.exec(cmd, timeout=timeout)
                if not result.success:
                    raise ExecutionError(
                        f"Setup command failed: {cmd}",
                        exit_code=result.exit_code,
                        stdout=result.stdout,
                        stderr=result.stderr,
                    )

            # Execute for each input
            for item in inputs:
                if isinstance(item, tuple):
                    args = item
                else:
                    args = (item,)
                result = self._execute_function(session, args, {}, timeout)
                results.append(result)

        return results

    def starmap(
        self,
        inputs: list[tuple[tuple[Any, ...], dict[str, Any]]],
        *,
        timeout: float | None = None,
    ) -> list[T]:
        """Execute the function with args and kwargs for multiple inputs.

        Args:
            inputs: List of (args, kwargs) tuples.
            timeout: Per-execution timeout in seconds.

        Returns:
            List of results in the same order as inputs.

        Example:
            @sandbox.function()
            def greet(name, greeting="Hello"):
                return f"{greeting}, {name}!"

            results = greet.starmap([
                (("Alice",), {"greeting": "Hi"}),
                (("Bob",), {}),
            ])
        """
        results: list[T] = []
        with self._sandbox.run() as session:
            for cmd in self._setup_commands:
                result = session.exec(cmd, timeout=timeout)
                if not result.success:
                    raise ExecutionError(
                        f"Setup command failed: {cmd}",
                        exit_code=result.exit_code,
                        stdout=result.stdout,
                        stderr=result.stderr,
                    )

            for args, kwargs in inputs:
                result = self._execute_function(session, args, kwargs, timeout)
                results.append(result)

        return results

    def __repr__(self) -> str:
        return f"<RemoteFunction {self._func.__name__} sandbox={self._sandbox._name!r}>"


def create_function_decorator(
    sandbox: Sandbox,
    *,
    setup_commands: list[str] | None = None,
) -> Callable[[Callable[..., T]], RemoteFunction[T]]:
    """Create a decorator for remote function execution.

    Args:
        sandbox: The sandbox configuration.
        setup_commands: Commands to run before the function.

    Returns:
        A decorator that wraps functions in RemoteFunction.
    """

    def decorator(func: Callable[..., T]) -> RemoteFunction[T]:
        return RemoteFunction(
            func,
            sandbox,
            setup_commands=setup_commands,
            is_command=False,
        )

    return decorator


def create_command_decorator(
    sandbox: Sandbox,
    command: str,
    *,
    setup_commands: list[str] | None = None,
) -> Callable[[Callable[..., Any]], RemoteFunction[ExecResult]]:
    """Create a decorator for remote command execution.

    Args:
        sandbox: The sandbox configuration.
        command: Shell command template.
        setup_commands: Commands to run before the main command.

    Returns:
        A decorator that wraps functions in RemoteFunction.
    """

    def decorator(func: Callable[..., Any]) -> RemoteFunction[ExecResult]:
        return RemoteFunction(
            func,
            sandbox,
            setup_commands=setup_commands,
            is_command=True,
            command_template=command,
        )

    return decorator
