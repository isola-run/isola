#!/usr/bin/env python3
"""
Quick example showing Isola sandbox usage
Run the mock server first: python demo/mock_server.py
"""

from isola_client import IsolaClient, SandboxConfig, SandboxClass

def main():
    print("Isola Sandbox Quick Example")
    print("-" * 40)
    
    # Connect to the API
    client = IsolaClient(base_url="http://localhost:3000")
    
    # Check server health
    health = client.health_check()
    print(f"✓ Server status: {health['status']}")
    
    # Create a sandbox with context manager for automatic cleanup
    config = SandboxConfig(
        name="quick-demo",
        sandbox_class=SandboxClass.SMALL,
        labels={"demo": "quick-example"}
    )
    
    print(f"\nCreating sandbox '{config.name}'...")
    
    with client.sandbox(config) as sandbox:
        print(f"✓ Sandbox created (ID: {sandbox.sandbox_id[:8]}...)")
        
        # Example 1: Simple Python calculation
        print("\n1. Running Python calculation:")
        code = """
import math

# Calculate circle area
radius = 10
area = math.pi * radius ** 2
print(f"Circle with radius {radius} has area: {area:.2f}")

# Generate fibonacci
def fibonacci(n):
    a, b = 0, 1
    result = []
    for _ in range(n):
        result.append(a)
        a, b = b, a + b
    return result

fib_sequence = fibonacci(10)
print(f"First 10 Fibonacci numbers: {fib_sequence}")
"""
        result = sandbox.execute_python(code)
        print("   Output:")
        for line in result["output"].strip().split("\n"):
            print(f"   > {line}")
        
        # Example 2: System information
        print("\n2. Getting system information:")
        result = sandbox.execute_bash("uname -a && python3 --version && pwd")
        print("   Output:")
        for line in result["output"].strip().split("\n"):
            print(f"   > {line}")
        
        # Example 3: Working with files
        print("\n3. Creating and reading files:")
        
        # Create a file
        create_file_code = """
# Write some data to a file
with open('data.txt', 'w') as f:
    f.write("Hello from Isola sandbox!\\n")
    f.write("This file was created dynamically.\\n")
    f.write("Sandbox environments are isolated and secure.\\n")

print("File created successfully!")
"""
        sandbox.execute_python(create_file_code)
        
        # Read and process the file
        process_file_code = """
# Read and analyze the file
with open('data.txt', 'r') as f:
    content = f.read()
    lines = content.strip().split('\\n')
    
print(f"File contents ({len(lines)} lines, {len(content)} characters):")
print("-" * 40)
print(content)
print("-" * 40)

# Word frequency
words = content.lower().split()
print(f"Total words: {len(words)}")
print(f"Unique words: {len(set(words))}")
"""
        result = sandbox.execute_python(process_file_code)
        print("   Output:")
        for line in result["output"].strip().split("\n"):
            print(f"   > {line}")
        
        # Example 4: Installing packages (simulated)
        print("\n4. Package management (simulated):")
        package_code = """
import sys
import subprocess

# Check Python version
print(f"Python version: {sys.version.split()[0]}")

# List installed packages (simulated - would use pip in real sandbox)
standard_packages = ['math', 'json', 'datetime', 'collections', 'itertools']
print(f"Available standard packages: {', '.join(standard_packages)}")

# Import and use a package
from collections import Counter
data = ['apple', 'banana', 'apple', 'orange', 'banana', 'apple']
counts = Counter(data)
print(f"Fruit counts: {dict(counts)}")
"""
        result = sandbox.execute_python(package_code)
        print("   Output:")
        for line in result["output"].strip().split("\n"):
            print(f"   > {line}")
    
    print(f"\n✓ Sandbox automatically cleaned up")
    print("\nQuick example completed successfully!")


if __name__ == "__main__":
    import sys
    try:
        main()
    except Exception as e:
        print(f"\nError: {e}")
        print("\nMake sure the mock server is running:")
        print("  python demo/mock_server.py")
        sys.exit(1)
