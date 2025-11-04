# Isola Sandbox Infrastructure Demo

A demonstration of the Isola Sandbox Infrastructure API, featuring a mock server implementation and a Python client for creating and managing sandboxes with code execution capabilities.

## Features

- **Mock API Server**: FastAPI-based server implementing the Isola API specification
- **Python Client**: Comprehensive client library for interacting with the Isola API
- **Sandbox Management**: Create, start, stop, and delete sandboxes
- **Code Execution**: Execute Python and Bash code within isolated sandboxes
- **Context Managers**: Automatic sandbox lifecycle management
- **Parallel Execution**: Run code in multiple sandboxes simultaneously

## Installation

### Using uv (recommended)

```bash
# Install dependencies
uv pip install -e .

# Or install with dev dependencies
uv pip install -e ".[dev]"
```

### Using pip

```bash
# Install dependencies
pip install -r requirements.txt

# Or install as package
pip install -e .
```

## Quick Start

### Option 1: Run Everything with the Launch Script

```bash
chmod +x demo/run_demo.sh
./demo/run_demo.sh
```

This will:
1. Start the mock server on port 3000
2. Launch the interactive demo client
3. Automatically cleanup when done

### Option 2: Run Components Separately

1. **Start the Mock Server**:
```bash
python demo/mock_server.py
```

The server will start on `http://localhost:3000`
- API Documentation: `http://localhost:3000/docs`
- Interactive API Explorer: `http://localhost:3000/redoc`

2. **Run the Demo Client** (in another terminal):
```bash
python demo/demo.py
```

## Usage Examples

### Basic Client Usage

```python
from demo.isola_client import IsolaClient, SandboxConfig, SandboxClass

# Initialize client
client = IsolaClient(base_url="http://localhost:3000")

# Create a sandbox
config = SandboxConfig(
    name="my-sandbox",
    sandbox_class=SandboxClass.SMALL,
    cpu=2,
    memory=4,
    labels={"project": "demo"}
)

sandbox = client.create_sandbox(config)
sandbox_id = sandbox["id"]

# Execute Python code
result = client.execute_python(sandbox_id, """
import math
print(f"Pi is approximately {math.pi:.5f}")
""")
print(result["stdout"])

# Execute Bash commands
result = client.execute_bash(sandbox_id, "echo 'Hello from sandbox!'")
print(result["stdout"])

# Clean up
client.delete_sandbox(sandbox_id, force=True)
```

### Using Context Manager

```python
from demo.isola_client import IsolaClient, SandboxConfig

client = IsolaClient()
config = SandboxConfig(name="temp-sandbox")

# Sandbox is automatically created and cleaned up
with client.sandbox(config) as sandbox_ctx:
    # Execute code
    result = sandbox_ctx.execute_python("""
        for i in range(5):
            print(f"Count: {i}")
    """)
    print(result["stdout"])
```

## Demo Features

The interactive demo includes:

1. **Basic Operations**: Create, list, start, stop, and delete sandboxes
2. **Code Execution**: Run Python code and bash commands in sandboxes
3. **Context Manager**: Automatic sandbox lifecycle management
4. **Parallel Execution**: Execute code in multiple sandboxes simultaneously
5. **Error Handling**: Demonstration of error scenarios and handling

## API Endpoints

The mock server implements these key endpoints:

- `GET /health` - Health check
- `GET /config` - System configuration
- `POST /sandboxes` - Create a new sandbox
- `GET /sandboxes` - List sandboxes
- `GET /sandboxes/{id}` - Get sandbox details
- `DELETE /sandboxes/{id}` - Delete a sandbox
- `POST /sandboxes/{id}/start` - Start a sandbox
- `POST /sandboxes/{id}/stop` - Stop a sandbox
- `POST /sandboxes/{id}/restart` - Restart a sandbox
- `POST /sandboxes/{id}/execute` - Execute code in a sandbox (extended API)

## Project Structure

```
demo/
├── isola-api-spec.yaml    # OpenAPI specification
├── mock_server.py         # Mock API server implementation
├── isola_client.py        # Python client library
├── demo.py                # Interactive demo script
└── run_demo.sh           # Launch script for demo
```

## Development

### Running Tests

```bash
# Run tests (when available)
pytest tests/
```

### Code Formatting

```bash
# Format code with black
black demo/
```

### Type Checking

```bash
# Run mypy for type checking
mypy demo/
```

## License

MIT License - See LICENSE file for details

## Notes

This is a demonstration implementation for educational and testing purposes. The mock server simulates sandbox functionality but does not provide true isolation. For production use, implement proper containerization or virtualization for secure sandbox isolation.