#!/usr/bin/env python3
"""Example: Remote function execution using decorators.

This example demonstrates how to use Isola's decorator-based API
to execute Python functions remotely in isolated sandboxes.

Prerequisites:
    - Set ISOLA_API_KEY environment variable
    - Set ISOLA_BASE_URL for dev environment (default is localhost:8080)

Usage:
    export ISOLA_API_KEY="iso_sk_demo"
    export ISOLA_BASE_URL="http://localhost:30080"  # For dev environment
    python decorator_example.py
"""

import isola

# =============================================================================
# Basic Example: Simple function execution
# =============================================================================

# Define sandbox configuration using the builder pattern
sandbox = (
    isola.Sandbox("data-processor")
    .image("python:3.11-slim")
    .cpu(1)
    .memory(2)
    .env({"PYTHONUNBUFFERED": "1"})
)


# Decorate a function for remote execution
# Note: cloudpickle is required for function serialization
@sandbox.function(setup_commands=["pip install -q cloudpickle"])
def compute_sum(numbers: list[int]) -> int:
    """Compute sum of numbers - runs in the sandbox."""
    return sum(numbers)


# =============================================================================
# Example with setup commands (pip install)
# =============================================================================

ml_sandbox = (
    isola.Sandbox("ml-runner")
    .image("python:3.11-slim")
    .cpu(2)
    .memory(4)
)


@ml_sandbox.function(setup_commands=["pip install -q cloudpickle numpy"])
def analyze_data(data: list[float]) -> dict:
    """Analyze data using numpy - runs in the sandbox after pip install."""
    import numpy as np

    arr = np.array(data)
    return {
        "mean": float(np.mean(arr)),
        "std": float(np.std(arr)),
        "min": float(np.min(arr)),
        "max": float(np.max(arr)),
    }


# =============================================================================
# Example with map: Process multiple inputs efficiently
# =============================================================================

@sandbox.function(setup_commands=["pip install -q cloudpickle"])
def square(x: int) -> int:
    """Square a number."""
    return x * x


# =============================================================================
# Example: Shell command wrapper
# =============================================================================

@sandbox.command("echo 'Processing...' && ls -la /tmp")
def list_tmp_files():
    """Wrapper for shell command execution."""
    pass


# =============================================================================
# Main: Run examples
# =============================================================================

if __name__ == "__main__":
    print("=" * 60)
    print("Isola SDK - Decorator Examples")
    print("=" * 60)

    # Example 1: Basic remote execution
    print("\n1. Basic remote execution:")
    print(f"   compute_sum([1, 2, 3, 4, 5])")
    result = compute_sum.remote([1, 2, 3, 4, 5])
    print(f"   Result: {result}")

    # Example 2: With setup commands
    print("\n2. Function with numpy (pip install in sandbox):")
    print(f"   analyze_data([1.5, 2.3, 3.7, 4.1, 5.9])")
    stats = analyze_data.remote([1.5, 2.3, 3.7, 4.1, 5.9])
    print(f"   Result: {stats}")

    # Example 3: Map over multiple inputs (single sandbox, multiple executions)
    print("\n3. Map over multiple inputs (efficient batch processing):")
    print(f"   square.map([1, 2, 3, 4, 5])")
    squares = square.map([1, 2, 3, 4, 5])
    print(f"   Results: {squares}")

    # Example 4: Local execution (decorated functions still work locally)
    print("\n4. Local execution (no sandbox):")
    print(f"   compute_sum.local([10, 20, 30])")
    local_result = compute_sum.local([10, 20, 30])
    print(f"   Result: {local_result}")

    # Example 5: Shell command
    print("\n5. Shell command wrapper:")
    print("   list_tmp_files.remote()")
    cmd_result = list_tmp_files.remote()
    print(f"   Exit code: {cmd_result.exit_code}")
    print(f"   Output:\n{cmd_result.stdout}")

    print("\n" + "=" * 60)
    print("All examples completed!")
    print("=" * 60)
