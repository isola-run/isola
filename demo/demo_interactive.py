#!/usr/bin/env python3
import sys
import time
from typing import Dict, Any
from isola_client import IsolaClient, SandboxConfig, SandboxClass, SandboxState


def main():    
    # Initialize client
    print("\nInitializing Isola client...")
    client = IsolaClient(base_url="http://localhost:3000")
    
    try:
        # Check if server is running
        health = client.health_check()
        if health["status"] != "healthy":
            print("ERROR: Server is not healthy!")
            return 1
        print("✓ Connected to Isola API server")
        
        # Create a sandbox for code execution
        print("\n1. Creating sandbox for code execution...")
        sandbox_config = SandboxConfig(
            name="code-executor",
            image="python:3.11",
            sandbox_class=SandboxClass.MEDIUM,
            cpu=2,
            memory=2,
            labels={"purpose": "code-execution"}
        )
        
        sandbox = client.create_sandbox(sandbox_config)
        sandbox_id = sandbox["id"]
        print(f"  Created sandbox: {sandbox['name']} ({sandbox_id[:8]}...)")
        
        # Wait for sandbox to be ready
        print("Waiting for sandbox to start...")
        if not client.wait_for_sandbox(sandbox_id, SandboxState.STARTED, timeout=30):
            print("  ERROR: Sandbox failed to start!")
            return 1

        # Execute Python code examples
        code = "print 'Hello, Sandbox!'"
        result = client.execute_python(sandbox_id, code)
        print(f"  Output: {result['stdout'].strip()}")
        print(f"  Execution time: {result['executionTime']:.3f}s")


    
    except Exception as e:
        print(f"\nUnexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    
    return 0


if __name__ == "__main__":
    sys.exit(main())
