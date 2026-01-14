#!/usr/bin/env python3
"""Example: Interactive sandbox sessions using context manager.

This example shows how to use the context manager API for
interactive sandbox operations like file uploads, command
execution, and file downloads.

Prerequisites:
    export ISOLA_API_KEY="iso_sk_demo"
    export ISOLA_BASE_URL="http://localhost:30080"  # For dev environment

Usage:
    python context_manager_example.py
"""

import isola

# Configure sandbox
sandbox = (
    isola.Sandbox("interactive-session")
    .image("python:3.11-slim")
    .cpu(1)
    .memory(2)
)


def main():
    print("=" * 60)
    print("Isola SDK - Context Manager Example")
    print("=" * 60)

    # Use context manager for automatic cleanup
    with sandbox.run() as session:
        print(f"\n✓ Sandbox created: {session.id}")
        print(f"  Name: {session.name}")
        print(f"  State: {session.state.value}")

        # Execute commands
        print("\n1. Running commands:")
        result = session.exec("python --version")
        print(f"   Python version: {result.stdout.strip()}")

        result = session.exec("uname -a")
        print(f"   System: {result.stdout.strip()}")

        # Upload a script
        print("\n2. Uploading a Python script:")
        script = '''
import json
import sys

data = {"message": "Hello from sandbox!", "numbers": [1, 2, 3, 4, 5]}
result = {"data": data, "sum": sum(data["numbers"])}

with open("/tmp/output.json", "w") as f:
    json.dump(result, f, indent=2)

print(json.dumps(result, indent=2))
'''
        session.upload_text(script, "/tmp/process.py")
        print("   Uploaded /tmp/process.py")

        # Execute the script
        print("\n3. Executing the script:")
        result = session.exec("python /tmp/process.py")
        print(f"   Output:\n{result.stdout}")

        # Download the result
        print("\n4. Downloading output file:")
        output = session.download_text("/tmp/output.json")
        print(f"   Content: {output}")

        # Check exit codes
        print("\n5. Checking exit codes:")
        result = session.exec("exit 0")
        print(f"   'exit 0' -> success={result.success}, code={result.exit_code}")

        result = session.exec("exit 1")
        print(f"   'exit 1' -> success={result.success}, code={result.exit_code}")

    # Sandbox is automatically terminated when exiting the context
    print("\n✓ Sandbox automatically terminated")
    print("\n" + "=" * 60)
    print("Done!")
    print("=" * 60)


if __name__ == "__main__":
    main()
