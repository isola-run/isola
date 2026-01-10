#!/usr/bin/env python3
"""
Isola Basic Usage Example

Demonstrates core sandbox operations using the Python SDK.

Usage:
    export ISOLA_API="http://localhost:8080"
    export ISOLA_API_KEY="iso_sk_demo"
    python basic_usage.py
"""

import json
import os
import sys

# Add the client to path (adjust as needed)
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../tests/client'))

try:
    from isola_client import IsolaClient
except ImportError:
    print("Error: isola_client not found. Install from tests/client/")
    print("  cd tests && pip install -e client/")
    sys.exit(1)


def main():
    # Configuration
    api_url = os.environ.get("ISOLA_API", "http://localhost:8080")
    api_key = os.environ.get("ISOLA_API_KEY", "iso_sk_demo")

    print("=== Isola Basic Usage Example ===")
    print(f"API: {api_url}\n")

    # Initialize client
    client = IsolaClient(api_url, api_key)

    # 1. Create sandbox
    print("1. Creating sandbox...")
    sandbox = client.create_sandbox(
        name="basic-example",
        template_name="python-sandbox",
        auto_start=True
    )
    sandbox_id = sandbox['id']
    print(f"   Created: {sandbox_id}")

    try:
        # 2. Wait for running
        print("2. Waiting for sandbox to be ready...")
        sandbox = client.wait_for_state(sandbox_id, "running", timeout=60)
        print("   Ready!")

        # 3. Execute simple command
        print("3. Executing command...")
        result = client.execute_command(sandbox_id, "python --version")
        print(f"   Python version: {result['stdout'].strip()}")

        # 4. Execute Python code
        print("4. Running Python code...")
        code = "print(sum(i**2 for i in range(10)))"
        result = client.execute_command(sandbox_id, f"python -c '{code}'")
        print(f"   Sum of squares 0-9: {result['stdout'].strip()}")

        # 5. Upload and run a script
        print("5. Upload and execute script...")
        script = '''
import json
import platform

info = {
    "python_version": platform.python_version(),
    "platform": platform.platform(),
    "processor": platform.processor() or "unknown"
}
print(json.dumps(info, indent=2))
'''
        client.upload_file(sandbox_id, "/workspace/info.py", script.encode())
        result = client.execute_command(sandbox_id, "python /workspace/info.py")
        info = json.loads(result['stdout'])
        print(f"   System info: {info}")

        # 6. Multi-step workflow
        print("6. Multi-step workflow...")

        # Install a package
        print("   Installing package...")
        result = client.execute_command(
            sandbox_id,
            "pip install --quiet faker",
            timeout=60
        )

        # Use the package
        faker_script = '''
from faker import Faker
fake = Faker()
for _ in range(3):
    print(f"Name: {fake.name()}, Email: {fake.email()}")
'''
        client.upload_file(sandbox_id, "/workspace/faker_demo.py", faker_script.encode())
        result = client.execute_command(sandbox_id, "python /workspace/faker_demo.py")
        print("   Generated data:")
        for line in result['stdout'].strip().split('\n'):
            print(f"     {line}")

        # 7. File operations
        print("7. File operations...")

        # Write data
        data = {"users": [{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]}
        client.upload_file(
            sandbox_id,
            "/workspace/data.json",
            json.dumps(data).encode()
        )

        # Process data
        process_script = '''
import json
with open("/workspace/data.json") as f:
    data = json.load(f)
for user in data["users"]:
    print(f"User {user['id']}: {user['name']}")
'''
        client.upload_file(sandbox_id, "/workspace/process.py", process_script.encode())
        result = client.execute_command(sandbox_id, "python /workspace/process.py")
        print(f"   Processed data:\n{result['stdout']}")

    finally:
        # 8. Cleanup
        print("8. Terminating sandbox...")
        client.terminate_sandbox(sandbox_id)
        print("   Done!")

    print("\n=== Example Complete ===")


if __name__ == "__main__":
    main()
