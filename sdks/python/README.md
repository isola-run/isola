# Isola Python SDK

Isola is an open-source sandbox platform for running untrusted and AI-generated code securely on your own Kubernetes cluster. This SDK lets you create sandboxes, execute commands, read and write files, and snapshot environments programmatically.

The SDK provides both synchronous and asynchronous clients.

## Install

```bash
pip install isola
```

Requires Python 3.10 or later.

## How it works

Before diving into code, here is a quick overview of the object model:

- `Isola` (or `AsyncIsola`) is the client. It holds the connection to your Isola instance.
- `client.sandboxes.create()` returns a `Sandbox`, an isolated container (or group of containers) where you run code.
- A `Sandbox` exposes `.commands` for executing processes and `.filesystem` for reading and writing files.
- Commands can be blocking (`run`, waits for completion) or non-blocking (`spawn`, streams output as it arrives).
- Rootfs snapshots are separate resources managed through `client.rootfs_snapshots`. They let you capture and restore a sandbox's filesystem.

## Quick start

Set the `ISOLA_BASE_URL` environment variable to point at your Isola instance:

```bash
export ISOLA_BASE_URL=http://localhost:8080
```

```python
from isola import Isola

client = Isola()

with client.sandboxes.create(image="alpine:3.21") as sandbox:
    result = sandbox.commands.run("echo", "hello world")
    print(result.stdout)      # "hello world\n"
    print(result.exit_code)   # 0
```

The `with` block deletes the sandbox automatically when it exits. You can also call `sandbox.delete()` manually.

## Async client

Import `AsyncIsola` instead of `Isola` and use `await` with each call:

```python
import asyncio
from isola import AsyncIsola

async def main():
    async with AsyncIsola() as client:
        async with await client.sandboxes.create(image="alpine:3.21") as sandbox:
            result = await sandbox.commands.run("echo", "hello async")
            print(result.stdout)

asyncio.run(main())
```

The async API is identical to the sync API. All examples below use the sync client.

## Sandbox options

Customize resources, environment variables, and the startup command:

```python
sandbox = client.sandboxes.create(
    image="python:3.12-slim",
    command=["sleep", "infinity"],
    env={"APP_ENV": "sandbox"},
    cpu=0.5,            # CPU cores
    memory=256,         # MiB
    ephemeral_storage=1024,  # MiB
    timeout_seconds=3600,
)
```

Skip waiting for the sandbox to be ready:

```python
sandbox = client.sandboxes.create(image="alpine:3.21", max_wait_seconds=0)
print(sandbox.status)  # might be SandboxStatus.PENDING
```

### Timeouts

There are three timeouts that control different things. They are easy to mix up, so here is what each one does:

| Timeout | Side | Default | What it controls |
|---------|------|---------|-----------------|
| `max_wait_seconds` | Client | 60s | How long `create()` polls before returning. Set to 0 to return immediately. Raises `IsolaTimeoutError` if it expires. The sandbox keeps running on the server regardless. |
| `startup_timeout_seconds` | Server | 60s | How long the server gives the container to start (image pull, scheduling). If it expires, the sandbox is marked Failed. |
| `timeout_seconds` | Server | No limit | Maximum lifetime of the sandbox. The server terminates it after this duration. |

> Sandboxes have **no network access by default**. See [Network configuration](#network-configuration) to enable it.

## Commands

### Run (blocking)

`run()` executes a command and waits for it to finish:

```python
result = sandbox.commands.run("echo", "hello world")
print(result.stdout)      # "hello world\n"
print(result.stderr)      # ""
print(result.exit_code)   # 0
```

Pass options as keyword arguments:

```python
result = sandbox.commands.run(
    "ls", "-la",
    env={"HOME": "/root"},
    cwd="/tmp",
    timeout_seconds=30,
)
```

### Spawn (non-blocking)

`spawn()` starts a command and returns immediately. Stream output as it arrives:

```python
cmd = sandbox.commands.spawn("sh", "-c", "for i in 1 2 3; do echo line$i; sleep 1; done")
for chunk in cmd.stdout:
    print(chunk, end="")
exit_code = cmd.wait()
```

### Stdin

For simple cases, pass `input` to `run()`:

```python
result = sandbox.commands.run("cat", input="hello from stdin\n")
print(result.stdout)  # "hello from stdin\n"
```

For interactive control, use `write_stdin()` and `close_stdin()` on a spawned command:

```python
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
# Write text
sandbox.filesystem.write("/tmp/hello.txt", "hello world")

# Write binary data
sandbox.filesystem.write("/tmp/data.bin", b"\x00\x01\x02")

# Upload a local file
with open("local.tar.gz", "rb") as f:
    sandbox.filesystem.write("/tmp/archive.tar.gz", f)

# Read a file (returns bytes)
data = sandbox.filesystem.read("/tmp/hello.txt")
print(data.decode())  # "hello world"
```

Parent directories are created automatically.

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

# Delete explicitly (instead of using a context manager)
sandbox.delete()
```

## Network configuration

Sandboxes have no network access by default. Pass a `Network` object to `create()` to open things up.

Allow full internet access:

```python
from isola import Network

sandbox = client.sandboxes.create(
    image="alpine:3.21",
    network=Network(allow_internet_egress=True),
)
```

When internet egress or custom CIDRs are enabled without cluster DNS, the server automatically configures public nameservers (8.8.8.8, 1.1.1.1) so DNS resolution works out of the box.

Other network options:

```python
Network(
    allow_internet_egress=True,     # allow outbound internet traffic
    allow_cluster_dns=True,         # use the cluster's DNS service
    allow_ipv6_egress=True,         # allow outbound IPv6
    allowed_egress_cidrs=["10.0.0.0/8"],  # fine-grained CIDR allowlist
    nameservers=["8.8.8.8"],        # custom DNS nameservers
)
```

## Rootfs snapshots

Rootfs snapshots capture a sandbox's filesystem so you can restore it later in a new sandbox. This is useful for pre-warming environments: install dependencies once, snapshot, then spin up fresh sandboxes from that snapshot in seconds.

### Create a snapshot

```python
snapshot = client.rootfs_snapshots.create(
    sandbox_id=sandbox.id,
    snapshot_name="my-snapshot",
)
print(snapshot.status)  # RootfsSnapshotStatus.SUCCEEDED
```

`create()` blocks until the snapshot completes (up to 300 seconds by default). Pass `max_wait_seconds=0` to return immediately.

### Restore from a snapshot

```python
restored = client.sandboxes.create(
    image="alpine:3.21",
    rootfs_snapshot_name="my-snapshot",
)
```

### Full round-trip example

```python
from isola import Isola

client = Isola()

# 1. Create a sandbox and install something
with client.sandboxes.create(image="python:3.12-slim") as sandbox:
    sandbox.commands.run("pip", "install", "requests")

    # 2. Snapshot the state
    snapshot = client.rootfs_snapshots.create(
        sandbox_id=sandbox.id,
        snapshot_name="python-with-requests",
    )

# 3. Spin up a new sandbox from the snapshot
with client.sandboxes.create(
    image="python:3.12-slim",
    rootfs_snapshot_name="python-with-requests",
) as sandbox:
    result = sandbox.commands.run("python", "-c", "import requests; print(requests.__version__)")
    print(result.stdout)
```

### Automatic snapshots on termination

You can also configure a sandbox to snapshot automatically when it terminates:

```python
from isola import SnapshotRootfs

sandbox = client.sandboxes.create(
    image="alpine:3.21",
    termination_policy=SnapshotRootfs(snapshot_name="on-exit-snapshot"),
)
```

### Checking snapshot status

```python
snapshot = client.rootfs_snapshots.get(snapshot.id)
print(snapshot.status)  # RootfsSnapshotStatus.SUCCEEDED
```

## Multi-container sandboxes

For advanced use cases, you can run multiple containers in a single sandbox. Use the `containers` parameter instead of `image`:

```python
from isola import Container

sandbox = client.sandboxes.create(
    containers=[
        Container(
            name="app",
            image="python:3.12-slim",
            command=["python", "-m", "http.server", "8080"],
        ),
        Container(
            name="worker",
            image="alpine:3.21",
            command=["sleep", "infinity"],
        ),
    ],
)
```

Target a specific container when running commands or writing files:

```python
result = sandbox.commands.run("curl", "http://localhost:8080", container="worker")
sandbox.filesystem.write("/tmp/data.txt", "hello", container="app")
```

You cannot mix `image` with `containers`. Per-container options like `command`, `env`, `cpu`, `memory`, and `rootfs_snapshot_name` go on each `Container` object.

## Error handling

All exceptions inherit from `IsolaError`:

```
IsolaError
├── APIError (has .status_code and .message)
│   ├── BadRequestError (400)
│   ├── NotFoundError (404)
│   ├── ConflictError (409)
│   ├── ValidationError (422)
│   ├── InternalError (500)
│   └── BadGatewayError (502)
├── IsolaTimeoutError
└── APIConnectionError
```

```python
from isola import IsolaError, IsolaTimeoutError, NotFoundError, APIConnectionError

try:
    sandbox = client.sandboxes.get("nonexistent")
except NotFoundError as e:
    print(e.status_code)  # 404
    print(e.message)
except IsolaTimeoutError:
    print("Timed out waiting")
except APIConnectionError:
    print("Could not reach the API")
except IsolaError:
    print("Something else went wrong")
```

The SDK automatically retries on transient errors (502, 503, 504 and connection failures), up to 5 times with a 1-second delay between attempts.

## Configuration

The base URL can be set via environment variable or constructor argument:

```python
# From environment variable (recommended)
client = Isola()  # reads ISOLA_BASE_URL

# Explicit
client = Isola(base_url="http://localhost:8080")

# Both clients support context managers
with Isola() as client:
    ...
```
