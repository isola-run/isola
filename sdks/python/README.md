# isola Python SDK

Python SDK for the Isola sandbox API.

## Install

```bash
pip install isola
```

## Quick start

Set the `ISOLA_BASE_URL` environment variable:

```bash
export ISOLA_BASE_URL=http://localhost:8080
```

```python
from isola import Isola

client = Isola()
sandbox = client.sandboxes.create(image="alpine:3.21")

# Run a command and get output
result = sandbox.commands.run("echo", "hello world")
print(result.stdout)      # "hello world\n"
print(result.exit_code)   # 0

sandbox.delete()
```

## Sandbox options

```python
from isola import NetworkSpec

sandbox = client.sandboxes.create(
    image="python:3.12",
    command=["python", "-m", "http.server"],
    env={"PORT": "8080"},
    cpu="500m",
    memory="256Mi",
    ephemeral_storage="1Gi",
    timeout=3600,  # max lifetime in seconds
    network=NetworkSpec(
        allow_internet_egress=True,
    ),
)
```

## Commands

### Run (blocking)

```python
result = sandbox.commands.run("echo", "hello world")
print(result.stdout)      # "hello world\n"
print(result.stderr)      # ""
print(result.exit_code)   # 0

# With options
result = sandbox.commands.run("ls", env={"HOME": "/root"}, cwd="/tmp", timeout=30)
```

### Spawn (non-blocking)

```python
cmd = sandbox.commands.spawn("sh", "-c", "for i in 1 2 3; do echo line$i; sleep 1; done")
for chunk in cmd.stdout:
    print(chunk, end="")
exit_code = cmd.wait()
```

### Stdin

```python
# Pipe input to a command
result = sandbox.commands.run("cat", input="hello from stdin\n")
print(result.stdout)  # "hello from stdin\n"

# Manual stdin control
cmd = sandbox.commands.spawn("cat")
cmd.write_stdin("hello\n")
cmd.close_stdin()
cmd.wait()
print(cmd.stdout.read())  # "hello\n"
```

### Command control

```python
cmd = sandbox.commands.spawn("sleep", "60")
cmd.exit_code()  # None (still running)
cmd.kill()
cmd.wait()       # returns exit code
```

## File I/O

```python
result = sandbox.filesystem.write("/tmp/hello.txt", b"hello world")
print(result.absolute_path)   # "/tmp/hello.txt"
print(result.bytes_written)   # 11

data = sandbox.filesystem.read("/tmp/hello.txt")  # bytes
```

## Sandbox management

```python
# List all sandboxes
summaries = client.sandboxes.list()
for s in summaries:
    print(s.id, s.status)

# Get a sandbox by ID
sandbox = client.sandboxes.get("sandbox-id")
print(sandbox.status)              # SandboxStatus.RUNNING
print(sandbox.creation_timestamp)  # datetime

# Delete
sandbox.delete()
```

## Async client

```python
from isola import AsyncIsola

async with AsyncIsola() as client:
    sandbox = await client.sandboxes.create(image="alpine:3.21")

    result = await sandbox.commands.run("echo", "hello")
    print(result.stdout)

    # Async streaming
    cmd = await sandbox.commands.spawn("sh", "-c", "echo hello; sleep 1; echo world")
    async for chunk in cmd.stdout:
        print(chunk, end="")
    await cmd.wait()

    await sandbox.delete()
```

## Error handling

```python
from isola import IsolaError, NotFoundError, APIConnectionError

try:
    sandbox = client.sandboxes.get("nonexistent")
except NotFoundError as e:
    print(e.status_code)  # 404
    print(e.message)
except APIConnectionError:
    print("Could not connect to API")
except IsolaError:
    print("Something went wrong")
```

## Configuration

The base URL can be set via environment variable or constructor argument:

```python
# From environment variable (recommended)
client = Isola()  # reads ISOLA_BASE_URL

# Explicit
client = Isola(base_url="http://localhost:8080")

# Both sync and async clients support context managers
with Isola() as client:
    sandbox = client.sandboxes.create(image="alpine:3.21")
```
