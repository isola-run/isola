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

# Sandboxes are context managers — auto-deleted on exit
with client.sandboxes.create(image="alpine:3.21") as sandbox:
    result = sandbox.commands.run("echo", "hello world")
    print(result.stdout)      # "hello world\n"
    print(result.exit_code)   # 0
```

## Sandbox options

```python
from isola import Network

sandbox = client.sandboxes.create(
    image="python:3.12",
    command=["python", "-m", "http.server"],
    env={"PORT": "8080"},
    cpu=0.5,                # CPU cores (float)
    memory=256,             # MiB (int)
    ephemeral_storage=1024, # MiB (int)
    timeout_seconds=3600,  # max lifetime in seconds
    max_wait_seconds=120,   # wait up to 120s for sandbox to be ready (default: 60)
    network=Network(
        allow_internet_egress=True,
    ),
)

# Don't wait for ready — return immediately
sandbox = client.sandboxes.create(image="alpine:3.21", max_wait_seconds=0)
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
# Write text (str) or binary data (bytes)
sandbox.filesystem.write("/tmp/hello.txt", "hello world")
sandbox.filesystem.write("/tmp/data.bin", b"\x00\x01\x02")

# Upload a local file
with open("local.tar.gz", "rb") as f:
    sandbox.filesystem.write("/tmp/archive.tar.gz", f)

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

## Rootfs snapshots

```python
from isola import IsolaError, RootfsSnapshotStatus

snapshot = client.rootfs_snapshots.create(
    sandbox_id=sandbox.id,
    snapshot_name="my-snapshot",
    max_wait_seconds=300,
)

print(snapshot.status)  # RootfsSnapshotStatus.SUCCEEDED

# Fetch the latest snapshot state by ID
snapshot = client.rootfs_snapshots.get(snapshot.id)

# Restore a new sandbox from the snapshot name
restored = client.sandboxes.create(
    image="alpine:3.21",
    rootfs_snapshot_name="my-snapshot",
)
```

`rootfs_snapshots.create()` waits up to 300 seconds for completion by default. Pass `max_wait_seconds=0` to return immediately, or a custom value to adjust the client-side wait. If the snapshot reaches `failed` while waiting, `create()` raises `IsolaError`.

## Async client

```python
from isola import AsyncIsola

async with AsyncIsola() as client:
    async with await client.sandboxes.create(image="alpine:3.21") as sandbox:
        result = await sandbox.commands.run("echo", "hello")
        print(result.stdout)

        # Async streaming
        cmd = await sandbox.commands.spawn("sh", "-c", "echo hello; sleep 1; echo world")
        async for chunk in cmd.stdout:
            print(chunk, end="")
        await cmd.wait()
```

## Error handling

```python
from isola import APIConnectionError, IsolaError, IsolaTimeoutError, NotFoundError

try:
    sandbox = client.sandboxes.get("nonexistent")
except NotFoundError as e:
    print(e.status_code)  # 404
    print(e.message)
except IsolaTimeoutError:
    print("Timed out waiting for completion")
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
