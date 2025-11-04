#!/usr/bin/env python3
import sys
from isola_client import IsolaClient, SandboxConfig, SandboxState


def main():    
    # Initialize client
    client = IsolaClient(base_url="http://localhost:3000")
    
    try:        
        # Create a sandbox
        sandbox_config = SandboxConfig(
            name="code-executor",
            image="python:3.11",
            cpu=2.0,
            memory=2.0,
            labels={"purpose": "code-execution"}
        )
        
        sandbox = client.create_sandbox(sandbox_config)
        sandbox_id = sandbox["id"]

        # Execute Python code in a sandbox
        code = "print ('Hello, Sandbox!')"
        result = client.execute_python(sandbox_id, code)
        print(f"Output: {result['output'].strip()}")
        print(f"Execution time: {result['executionTime']:.3f}s")

        # Clean up
        client.delete_sandbox(sandbox_id)
    
    except Exception as e:
        print(f"\nUnexpected error: {e}")


if __name__ == "__main__":
    sys.exit(main())
