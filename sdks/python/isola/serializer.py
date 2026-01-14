"""Function serialization for remote execution.

This module handles serializing Python functions and their arguments
for execution in sandboxes using cloudpickle.
"""

from __future__ import annotations

import base64
import textwrap
from typing import Any, Callable

from .exceptions import SerializationError

# Runner script template that will be executed in the sandbox
RUNNER_SCRIPT = '''
import base64
import sys

def main():
    try:
        import cloudpickle
    except ImportError:
        print("ERROR: cloudpickle not installed. Run: pip install cloudpickle", file=sys.stderr)
        sys.exit(1)

    # Decode and load function
    func_data = base64.b64decode("{func_b64}")
    func = cloudpickle.loads(func_data)

    # Decode and load args/kwargs
    args_data = base64.b64decode("{args_b64}")
    args, kwargs = cloudpickle.loads(args_data)

    # Execute function
    try:
        result = func(*args, **kwargs)
    except Exception as e:
        # Serialize the exception
        exc_data = cloudpickle.dumps(e)
        print("__ISOLA_EXCEPTION__")
        print(base64.b64encode(exc_data).decode())
        sys.exit(1)

    # Serialize and output result
    result_data = cloudpickle.dumps(result)
    print("__ISOLA_RESULT__")
    print(base64.b64encode(result_data).decode())

if __name__ == "__main__":
    main()
'''

# Markers for parsing output
RESULT_MARKER = "__ISOLA_RESULT__"
EXCEPTION_MARKER = "__ISOLA_EXCEPTION__"


def serialize_function(func: Callable[..., Any]) -> bytes:
    """Serialize a function using cloudpickle.

    Args:
        func: The function to serialize.

    Returns:
        Serialized function as bytes.

    Raises:
        SerializationError: If serialization fails.
    """
    try:
        import cloudpickle
    except ImportError as e:
        raise SerializationError(
            "cloudpickle is required for function serialization. "
            "Install it with: pip install cloudpickle"
        ) from e

    try:
        return cloudpickle.dumps(func)
    except Exception as e:
        raise SerializationError(f"Failed to serialize function: {e}") from e


def serialize_args(args: tuple[Any, ...], kwargs: dict[str, Any]) -> bytes:
    """Serialize function arguments.

    Args:
        args: Positional arguments.
        kwargs: Keyword arguments.

    Returns:
        Serialized arguments as bytes.

    Raises:
        SerializationError: If serialization fails.
    """
    try:
        import cloudpickle
    except ImportError as e:
        raise SerializationError(
            "cloudpickle is required for argument serialization. "
            "Install it with: pip install cloudpickle"
        ) from e

    try:
        return cloudpickle.dumps((args, kwargs))
    except Exception as e:
        raise SerializationError(f"Failed to serialize arguments: {e}") from e


def deserialize_result(data: bytes) -> Any:
    """Deserialize a result from cloudpickle format.

    Args:
        data: Serialized result bytes.

    Returns:
        The deserialized result.

    Raises:
        SerializationError: If deserialization fails.
    """
    try:
        import cloudpickle
    except ImportError as e:
        raise SerializationError(
            "cloudpickle is required for result deserialization. "
            "Install it with: pip install cloudpickle"
        ) from e

    try:
        return cloudpickle.loads(data)
    except Exception as e:
        raise SerializationError(f"Failed to deserialize result: {e}") from e


def create_runner_script(func: Callable[..., Any], args: tuple[Any, ...], kwargs: dict[str, Any]) -> str:
    """Create a Python script that runs the serialized function.

    Args:
        func: The function to run.
        args: Positional arguments.
        kwargs: Keyword arguments.

    Returns:
        A complete Python script as a string.

    Raises:
        SerializationError: If serialization fails.
    """
    func_bytes = serialize_function(func)
    args_bytes = serialize_args(args, kwargs)

    func_b64 = base64.b64encode(func_bytes).decode("ascii")
    args_b64 = base64.b64encode(args_bytes).decode("ascii")

    return RUNNER_SCRIPT.format(func_b64=func_b64, args_b64=args_b64)


def parse_output(stdout: str) -> tuple[Any, Exception | None]:
    """Parse the output from a runner script execution.

    Args:
        stdout: The stdout from the runner script.

    Returns:
        A tuple of (result, exception). One will be None.

    Raises:
        SerializationError: If parsing or deserialization fails.
    """
    lines = stdout.strip().split("\n")

    # Find result or exception marker
    for i, line in enumerate(lines):
        if line.strip() == RESULT_MARKER:
            if i + 1 < len(lines):
                result_b64 = lines[i + 1].strip()
                try:
                    result_bytes = base64.b64decode(result_b64)
                    return deserialize_result(result_bytes), None
                except Exception as e:
                    raise SerializationError(f"Failed to decode result: {e}") from e

        elif line.strip() == EXCEPTION_MARKER:
            if i + 1 < len(lines):
                exc_b64 = lines[i + 1].strip()
                try:
                    exc_bytes = base64.b64decode(exc_b64)
                    exc = deserialize_result(exc_bytes)
                    return None, exc
                except Exception as e:
                    raise SerializationError(f"Failed to decode exception: {e}") from e

    # No marker found - might be a setup error
    raise SerializationError(
        f"Could not parse function output. The sandbox may not have cloudpickle installed. "
        f"Output was: {stdout[:500]}..."
    )


class FunctionSerializer:
    """Handles serialization of functions for remote execution.

    This class provides a higher-level interface for function serialization,
    with caching and validation.
    """

    def __init__(self) -> None:
        self._cache: dict[int, bytes] = {}

    def serialize(
        self,
        func: Callable[..., Any],
        args: tuple[Any, ...] = (),
        kwargs: dict[str, Any] | None = None,
    ) -> str:
        """Serialize a function call into a runner script.

        Args:
            func: The function to serialize.
            args: Positional arguments for the function.
            kwargs: Keyword arguments for the function.

        Returns:
            A Python script string ready to be executed.
        """
        kwargs = kwargs or {}
        return create_runner_script(func, args, kwargs)

    def parse_result(self, stdout: str, stderr: str, exit_code: int) -> Any:
        """Parse the result from function execution.

        Args:
            stdout: Standard output from execution.
            stderr: Standard error from execution.
            exit_code: Exit code from execution.

        Returns:
            The function's return value.

        Raises:
            ExecutionError: If the function raised an exception.
            SerializationError: If result parsing fails.
        """
        from .exceptions import ExecutionError

        if exit_code != 0:
            # Try to extract the serialized exception
            try:
                result, exc = parse_output(stdout)
                if exc is not None:
                    raise exc
            except SerializationError:
                pass

            # Fall back to generic execution error
            raise ExecutionError(
                f"Function execution failed with exit code {exit_code}",
                exit_code=exit_code,
                stdout=stdout,
                stderr=stderr,
            )

        result, exc = parse_output(stdout)
        if exc is not None:
            raise exc

        return result


# Module-level serializer instance
_serializer = FunctionSerializer()


def serialize_call(
    func: Callable[..., Any],
    args: tuple[Any, ...] = (),
    kwargs: dict[str, Any] | None = None,
) -> str:
    """Serialize a function call into a runner script.

    This is a convenience function that uses the module-level serializer.

    Args:
        func: The function to serialize.
        args: Positional arguments.
        kwargs: Keyword arguments.

    Returns:
        A Python script string.
    """
    return _serializer.serialize(func, args, kwargs)


def parse_result(stdout: str, stderr: str, exit_code: int) -> Any:
    """Parse the result from function execution.

    This is a convenience function that uses the module-level serializer.

    Args:
        stdout: Standard output from execution.
        stderr: Standard error from execution.
        exit_code: Exit code from execution.

    Returns:
        The function's return value.
    """
    return _serializer.parse_result(stdout, stderr, exit_code)
