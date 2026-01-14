# Isola SDK Examples

This directory contains examples demonstrating the Isola Python SDK.

## Prerequisites

Set your API key:

```bash
export ISOLA_API_KEY="iso_sk_demo"
```

Set the gateway URL (required for dev environment):

```bash
# For local dev environment (Tilt port forwarding)
export ISOLA_BASE_URL="http://localhost:30080"

# For production or other environments
export ISOLA_BASE_URL="http://isola-gw.example.com"
```

**Note:** The default base URL is `http://localhost:8080`, but in the dev environment
the gateway is forwarded to port `30080` via Tilt.

## Examples

### quickstart.py

The simplest possible example - define a function and run it remotely:

```bash
python quickstart.py
```

### decorator_example.py

Comprehensive examples of the decorator-based API:

- Basic function execution with `@sandbox.function()`
- Setup commands (e.g., `pip install`)
- Batch processing with `.map()`
- Shell command wrappers with `@sandbox.command()`
- Local vs remote execution

```bash
python decorator_example.py
```

### context_manager_example.py

Interactive sandbox sessions using the context manager:

- File uploads and downloads
- Command execution
- Exit code handling
- Automatic cleanup

```bash
python context_manager_example.py
```

## API Patterns

### Decorator Pattern (Recommended)

```python
import isola

sandbox = isola.Sandbox("my-sandbox").image("python:3.11-slim")

@sandbox.function()
def process(data):
    return data * 2

# Execute remotely
result = process.remote([1, 2, 3])

# Or locally
result = process.local([1, 2, 3])
```

### Context Manager Pattern

```python
import isola

sandbox = isola.Sandbox("my-sandbox").image("python:3.11-slim")

with sandbox.run() as session:
    session.upload("data.csv", "/tmp/data.csv")
    result = session.exec("python process.py")
    session.download("/tmp/output.csv", "output.csv")
```

### Low-Level Client

```python
import isola

client = isola.IsolaClient()
sandbox = client.create_sandbox(isola.SandboxConfig(name="test"))
result = client.execute_command(sandbox.id, "echo hello")
client.terminate_sandbox(sandbox.id)
```
