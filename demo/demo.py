#!/usr/bin/env python3
"""
Isola Sandbox Demo
Demonstrates creating sandboxes and executing code using the Isola API
"""
import sys
import time
from typing import Dict, Any
from isola_client import IsolaClient, SandboxConfig, SandboxClass, SandboxState


def print_header(title: str):
    """Print a formatted header"""
    print("\n" + "=" * 60)
    print(f" {title}")
    print("=" * 60)


def print_result(label: str, result: Any):
    """Print a formatted result"""
    print(f"\n{label}:")
    if isinstance(result, dict):
        for key, value in result.items():
            if key not in ["env", "labels"]:  # Skip verbose fields
                print(f"  {key}: {value}")
    else:
        print(f"  {result}")


def demo_basic_operations(client: IsolaClient):
    """Demonstrate basic sandbox operations"""
    print_header("BASIC SANDBOX OPERATIONS")
    
    # 1. Check health
    print("\n1. Checking API health...")
    health = client.health_check()
    print_result("Health Status", health)
    
    # 2. Get system configuration
    print("\n2. Getting system configuration...")
    config = client.get_config()
    print_result("System Config", config)
    
    # 3. Create a sandbox
    print("\n3. Creating a new sandbox...")
    sandbox_config = SandboxConfig(
        name="demo-sandbox-1",
        sandbox_class=SandboxClass.SMALL,
        labels={"purpose": "demo", "team": "engineering"},
        env={"DEBUG": "true", "APP_ENV": "development"}
    )
    
    sandbox = client.create_sandbox(sandbox_config)
    sandbox_id = sandbox["id"]
    print_result("Created Sandbox", sandbox)
    
    # 4. List sandboxes
    print("\n4. Listing all sandboxes...")
    sandboxes = client.list_sandboxes()
    print(f"  Total sandboxes: {sandboxes['total']}")
    for s in sandboxes["items"]:
        print(f"  - {s['name']} ({s['id'][:8]}...): {s['state']}")
    
    # 5. Get sandbox details
    print("\n5. Getting sandbox details...")
    sandbox_details = client.get_sandbox(sandbox_id)
    print_result("Sandbox Details", sandbox_details)
    
    # 6. Stop the sandbox
    print("\n6. Stopping the sandbox...")
    stopped = client.stop_sandbox(sandbox_id)
    print(f"  Sandbox state: {stopped['state']}")
    
    # 7. Start the sandbox again
    print("\n7. Starting the sandbox...")
    started = client.start_sandbox(sandbox_id)
    print(f"  Sandbox state: {started['state']}")
    
    # Wait for it to be ready
    client.wait_for_sandbox(sandbox_id, SandboxState.STARTED)
    
    # 8. Delete the sandbox
    print("\n8. Deleting the sandbox...")
    client.delete_sandbox(sandbox_id, force=True)
    print("  Sandbox deleted successfully")


def demo_code_execution(client: IsolaClient):
    """Demonstrate code execution in sandboxes"""
    print_header("CODE EXECUTION IN SANDBOXES")
    
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
    print("  Waiting for sandbox to start...")
    if not client.wait_for_sandbox(sandbox_id, SandboxState.STARTED, timeout=30):
        print("  ERROR: Sandbox failed to start!")
        return
    print("  Sandbox is ready!")
    
    try:
        # Execute Python code examples
        print("\n2. Executing Python code examples:")
        
        # Example 1: Simple calculation
        print("\n  a) Simple calculation:")
        code1 = """
import math
radius = 5
area = math.pi * radius ** 2
print(f"Area of circle with radius {radius}: {area:.2f}")
"""
        result = client.execute_python(sandbox_id, code1)
        print(f"     Output: {result['output'].strip()}")
        print(f"     Execution time: {result['executionTime']:.3f}s")
        
        # Example 2: Data processing
        print("\n  b) Data processing:")
        code2 = """
import json

data = {
    "users": [
        {"name": "Alice", "age": 30},
        {"name": "Bob", "age": 25},
        {"name": "Charlie", "age": 35}
    ]
}

average_age = sum(user["age"] for user in data["users"]) / len(data["users"])
print(f"Average age: {average_age:.1f}")
print(f"Youngest: {min(data['users'], key=lambda x: x['age'])['name']}")
print(f"Oldest: {max(data['users'], key=lambda x: x['age'])['name']}")
"""
        result = client.execute_python(sandbox_id, code2)
        print(f"     Output:\n{indent_lines(result['output'].strip(), 8)}")
        
        # Example 3: Error handling
        print("\n  c) Error handling:")
        code3 = """
def divide(a, b):
    return a / b

try:
    result = divide(10, 0)
except ZeroDivisionError as e:
    print(f"Error caught: {e}")
    print("Cannot divide by zero!")
"""
        result = client.execute_python(sandbox_id, code3)
        print(f"     Output: {result['output'].strip()}")
        
        # Execute Bash commands
        print("\n3. Executing Bash commands:")
        
        # Example 1: System information
        print("\n  a) System information:")
        result = client.execute_bash(sandbox_id, "uname -a && echo && python3 --version")
        print(f"     Output:\n{indent_lines(result['output'].strip(), 8)}")
        
        # Example 2: File operations
        print("\n  b) File operations:")
        commands = """
echo "Hello from Isola sandbox!" > test.txt
echo "This is a demo file." >> test.txt
echo "File contents:"
cat test.txt
echo ""
echo "File info:"
ls -la test.txt
"""
        result = client.execute_bash(sandbox_id, commands)
        print(f"     Output:\n{indent_lines(result['output'].strip(), 8)}")
        
    finally:
        # Cleanup
        print("\n4. Cleaning up...")
        client.delete_sandbox(sandbox_id, force=True)
        print("  Sandbox deleted successfully")


def demo_context_manager(client: IsolaClient):
    """Demonstrate using sandboxes with context managers"""
    print_header("CONTEXT MANAGER USAGE")
    
    print("\nUsing sandbox with context manager for automatic cleanup:")
    
    # Configure sandbox
    config = SandboxConfig(
        name="context-sandbox",
        sandbox_class=SandboxClass.SMALL,
        labels={"managed": "true"}
    )
    
    # Use context manager
    print("\n1. Creating managed sandbox...")
    with client.sandbox(config) as sandbox_ctx:
        print(f"  Sandbox created: {sandbox_ctx.sandbox['name']}")
        print(f"  Sandbox ID: {sandbox_ctx.sandbox_id[:8]}...")
        
        # Execute code
        print("\n2. Executing code in managed sandbox:")
        
        code = """
import random
import string

# Generate a random password
length = 12
chars = string.ascii_letters + string.digits + string.punctuation
password = ''.join(random.choice(chars) for _ in range(length))
print(f"Generated password: {password}")

# Calculate factorial
n = 10
factorial = 1
for i in range(1, n + 1):
    factorial *= i
print(f"Factorial of {n}: {factorial}")
"""
        
        result = sandbox_ctx.execute_python(code)
        print(f"  Output:\n{indent_lines(result['output'].strip(), 4)}")
        
        # Get sandbox info
        print("\n3. Getting sandbox info:")
        info = sandbox_ctx.get_info()
        print(f"  State: {info['state']}")
        print(f"  IP Address: {info['ipAddress']}")
        print(f"  Resources: {info['cpu']} CPU, {info['memory']}GB RAM, {info['disk']}GB Disk")
    
    print("\n4. Sandbox automatically cleaned up after context exit")


def demo_parallel_execution(client: IsolaClient):
    """Demonstrate parallel execution in multiple sandboxes"""
    print_header("PARALLEL EXECUTION IN MULTIPLE SANDBOXES")
    
    print("\nCreating multiple sandboxes for parallel execution:")
    
    # Create multiple sandboxes
    sandbox_ids = []
    num_sandboxes = 3
    
    try:
        for i in range(num_sandboxes):
            config = SandboxConfig(
                name=f"parallel-sandbox-{i+1}",
                sandbox_class=SandboxClass.SMALL,
                labels={"group": "parallel-demo"}
            )
            sandbox = client.create_sandbox(config)
            sandbox_ids.append(sandbox["id"])
            print(f"  Created sandbox {i+1}: {sandbox['name']}")
        
        # Wait for all to be ready
        print("\nWaiting for all sandboxes to start...")
        for sandbox_id in sandbox_ids:
            client.wait_for_sandbox(sandbox_id, SandboxState.STARTED)
        print("All sandboxes are ready!")
        
        # Execute different tasks in parallel
        print("\nExecuting different tasks in each sandbox:")
        
        tasks = [
            ("Calculate prime numbers", """
import math

def is_prime(n):
    if n < 2:
        return False
    for i in range(2, int(math.sqrt(n)) + 1):
        if n % i == 0:
            return False
    return True

primes = [n for n in range(2, 50) if is_prime(n)]
print(f"Prime numbers up to 50: {primes[:10]}...")
print(f"Total count: {len(primes)}")
"""),
            ("Generate Fibonacci sequence", """
def fibonacci(n):
    fib = [0, 1]
    for i in range(2, n):
        fib.append(fib[-1] + fib[-2])
    return fib

sequence = fibonacci(15)
print(f"Fibonacci sequence (first 15): {sequence}")
print(f"Sum: {sum(sequence)}")
"""),
            ("Analyze text", """
text = "The quick brown fox jumps over the lazy dog. This pangram contains all letters."

words = text.lower().split()
word_count = len(words)
unique_words = len(set(words))
char_count = len([c for c in text if c.isalpha()])

print(f"Word count: {word_count}")
print(f"Unique words: {unique_words}")
print(f"Letter count: {char_count}")
""")
        ]
        
        for i, (sandbox_id, (task_name, code)) in enumerate(zip(sandbox_ids, tasks)):
            print(f"\n  Sandbox {i+1} - {task_name}:")
            result = client.execute_python(sandbox_id, code)
            print(f"{indent_lines(result['output'].strip(), 4)}")
            print(f"    Execution time: {result['executionTime']:.3f}s")
        
    finally:
        # Cleanup all sandboxes
        print("\nCleaning up all sandboxes...")
        for sandbox_id in sandbox_ids:
            try:
                client.delete_sandbox(sandbox_id, force=True)
            except:
                pass
        print("All sandboxes deleted")


def demo_error_scenarios(client: IsolaClient):
    """Demonstrate error handling scenarios"""
    print_header("ERROR HANDLING SCENARIOS")
    
    print("\n1. Creating a sandbox for error demonstrations...")
    config = SandboxConfig(name="error-demo-sandbox")
    sandbox = client.create_sandbox(config)
    sandbox_id = sandbox["id"]
    
    # Wait for sandbox to be ready
    client.wait_for_sandbox(sandbox_id, SandboxState.STARTED)
    
    try:
        print("\n2. Testing various error scenarios:")
        
        # Syntax error
        print("\n  a) Python syntax error:")
        code_with_error = """
def broken_function(
    print("This has a syntax error")
"""
        result = client.execute_python(sandbox_id, code_with_error)
        print(f"     Exit code: {result['exitCode']}")
        print(f"     Output: {result['output'].strip() or 'No output'}")
        print(f"     Error: {result['error'] or 'No error message'}")
        
        # Runtime error
        print("\n  b) Python runtime error:")
        runtime_error_code = """
numbers = [1, 2, 3]
print(numbers[10])  # IndexError
"""
        result = client.execute_python(sandbox_id, runtime_error_code)
        print(f"     Exit code: {result['exitCode']}")
        print(f"     Output: {result['output'].strip()}")
        
        # Timeout scenario (if supported)
        print("\n  c) Execution timeout:")
        timeout_code = """
import time
print("Starting long-running task...")
time.sleep(5)  # This will timeout if timeout is less than 5
print("Task completed")
"""
        result = client.execute_python(sandbox_id, timeout_code, timeout=2)
        print(f"     Exit code: {result['exitCode']}")
        print(f"     Output: {result['output'].strip() or 'No output'}")
        print(f"     Error: {result['error'] or 'No error'}")
        
        # Invalid bash command
        print("\n  d) Invalid bash command:")
        result = client.execute_bash(sandbox_id, "nonexistentcommand --help")
        print(f"     Exit code: {result['exitCode']}")
        print(f"     Output: {result['output'].strip() or 'No output'}")
        print(f"     Error: {result['error'] or 'No error'}")
        
    finally:
        # Cleanup
        print("\n3. Cleaning up...")
        client.delete_sandbox(sandbox_id, force=True)
        print("  Sandbox deleted")


def indent_lines(text: str, spaces: int) -> str:
    """Indent each line of text by specified number of spaces"""
    indent = " " * spaces
    return "\n".join(indent + line for line in text.split("\n"))


def main():
    """Main demo function"""
    print("\n" + "=" * 60)
    print(" ISOLA SANDBOX INFRASTRUCTURE DEMO")
    print("=" * 60)
    
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
        
        # Run demos
        demos = [
            ("Basic Operations", demo_basic_operations),
            ("Code Execution", demo_code_execution),
            ("Context Manager", demo_context_manager),
            ("Parallel Execution", demo_parallel_execution),
            ("Error Handling", demo_error_scenarios),
        ]
        
        print("\nAvailable demos:")
        for i, (name, _) in enumerate(demos, 1):
            print(f"  {i}. {name}")
        print(f"  {len(demos)+1}. Run all demos")
        print("  0. Exit")
        
        while True:
            try:
                choice = input("\nSelect demo to run (0-6): ").strip()
                
                if choice == "0":
                    print("Exiting...")
                    break
                elif choice == str(len(demos)+1):
                    # Run all demos
                    for name, demo_func in demos:
                        print(f"\nRunning: {name}")
                        demo_func(client)
                        print("\nPress Enter to continue...")
                        input()
                    break
                elif choice.isdigit() and 1 <= int(choice) <= len(demos):
                    idx = int(choice) - 1
                    name, demo_func = demos[idx]
                    demo_func(client)
                else:
                    print("Invalid choice. Please try again.")
                    
            except KeyboardInterrupt:
                print("\nDemo interrupted by user")
                break
            except Exception as e:
                print(f"\nError during demo: {e}")
                import traceback
                traceback.print_exc()
        
    except requests.exceptions.ConnectionError:
        print("\nERROR: Cannot connect to Isola API server!")
        print("Please make sure the mock server is running:")
        print("  python demo/mock_server.py")
        return 1
    except Exception as e:
        print(f"\nUnexpected error: {e}")
        import traceback
        traceback.print_exc()
        return 1
    
    print("\nDemo completed successfully!")
    return 0


if __name__ == "__main__":
    sys.exit(main())
