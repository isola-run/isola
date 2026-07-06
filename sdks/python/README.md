# Isola Python SDK

Isola is an open-source sandbox platform for running untrusted and AI-generated code securely on your own Kubernetes cluster. This SDK lets you create sandboxes, execute commands, read and write files, and snapshot environments programmatically.

The SDK provides both synchronous and asynchronous clients.

## Install

```bash
pip install isola-run
```

Requires Python 3.10 or later.

## How it works

Before diving into code, here is a quick overview of the object model:

- `Isola` (or `AsyncIsola`) is the client. It holds the connections to your Isola instance.
- `client.sandboxes.create()` returns a `Sandbox`, where you run code.
- A `Sandbox` exposes `.commands` for executing processes and `.filesystem` for reading and writing files.
- Commands can be blocking (`run`, waits for completion) or non-blocking (`spawn`, streams output as it arrives).
- Rootfs snapshots are separate resources managed through `RootfsSnapshot`. They let you capture and restore a container's root filesystem changes.

## Quick start

`ISOLA_URL` must point at your Isola api-gateway. There is no default - set it for every deployment:

```bash
# Local development (kubectl port-forward)
export ISOLA_URL=http://localhost:8080

# In-cluster (same namespace)
export ISOLA_URL=http://isola-api-gateway

# In-cluster (cross-namespace)
export ISOLA_URL=http://isola-api-gateway.isola-system.svc.cluster.local

# External (ingress or load balancer)
export ISOLA_URL=https://isola.example.com
```

Or pass it directly: `Isola(url="http://isola-api-gateway.isola-system.svc.cluster.local")`

```python
from isola import Isola

with Isola() as client:
    sandbox = client.sandboxes.create(image="alpine:3.21")
    result = sandbox.commands.run("echo", "hello world")
    print(result.stdout)    # "hello world\n"
    print(result.exit_code) # 0
    sandbox.delete()
```

The `with` block closes the HTTP client automatically when it exits. You can also call `client.close()` instead.

Call `sandbox.delete()` when you are done with a sandbox. Two alternatives: use `with sandbox:` to delete automatically on exit, or set `timeout_seconds` on `create()` to let the server delete the sandbox after a fixed duration.

## Async client

Import `AsyncIsola` instead of `Isola` and use `await` with each call:

```python
from isola import AsyncIsola

async with AsyncIsola() as client:
    sandbox = await client.sandboxes.create(image="alpine:3.21")
    result = await sandbox.commands.run("echo", "hello async")
    print(result.stdout)
    await sandbox.delete()
```

The async API is identical to the sync API. All examples below use the sync client.

## Sandbox options

Customize resources, environment variables, and the startup command:

```python
sandbox = client.sandboxes.create(
    image="python:3.12-slim",
    command=["python", "-m", "http.server", "8080"],
    env={"PORT": "8080", "DEBUG": "1"},
    cpu=0.5,            # CPU cores
    memory=256,         # MiB
    ephemeral_storage=1024,  # MiB
    timeout_seconds=3600,   # auto-delete after 1 hour
)
```

Skip waiting for the sandbox to be ready:

```python
sandbox = client.sandboxes.create(image="alpine:3.21", max_wait_seconds=0)
print(sandbox.status)  # might be SandboxStatus.PENDING
```

> Sandboxes have **no network access by default**. See [Network configuration](#network-configuration) to enable it.

### Timeouts

| Timeout | Side | Default | What it controls |
|---------|------|---------|-----------------|
| `max_wait_seconds` | Client | 120s | How long `create()` polls before returning. Set to 0 to return immediately. Raises `IsolaTimeoutError` if it expires. The sandbox keeps running on the server regardless. |
| `startup_timeout_seconds` | Server | 90s | How long the server gives the sandbox to start. If it expires, the sandbox is marked Failed. Omit to use the server default. |
| `timeout_seconds` | Server | No limit | Maximum lifetime of the sandbox. The server begins the termination process after this duration. |

> Setting `timeout_seconds` (or using `with`) is strongly recommended to ensure the sandbox resource is eventually deleted from the k8s api-server.

## Commands

### Run (blocking)

`run()` executes a command and waits for it to finish:

```python
result = sandbox.commands.run("echo", "hello world")
print(result.stdout)      # "hello world\n"
print(result.stderr)      # ""
print(result.exit_code)   # 0
```

`run()` does not raise on non-zero exit codes. Always check `result.exit_code`.

```python
result = sandbox.commands.run(
    "ls", "-la",
    cwd="/tmp",                   # working directory for this command
    env={"LANG": "en_US.UTF-8"},  # merged with sandbox env
    timeout_seconds=30,           # SIGKILL after 30s
)
```

### Running scripts

**Shell scripts**, pass to `sh -c`:

```python
script = """
echo '== cpu ==';  nproc
echo '== mem ==';  free -h
echo '== disk =='; df -h /
"""
result = sandbox.commands.run("sh", "-c", script)
```

**Python code**, pass to `python3 -c`:

```python
code = """
import json, os
print(json.dumps({"cwd": os.getcwd(), "files": os.listdir(".")}))
"""
result = sandbox.commands.run("python3", "-c", code)
```

This is the natural pattern when executing LLM-generated code blocks. Both work with
multi-line strings, newlines are preserved and interpreted by the shell or Python
interpreter as statement separators.

> For commands you control, prefer separate args (`run("python3", "analyze.py", "--input", filename)`),
> it keeps data separate from the command itself.

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
sandbox.filesystem.write_text("/tmp/hello.txt", "Hello, World!")

# Write binary data
sandbox.filesystem.write_bytes("/tmp/data.bin", b"\x00\x01\x02")

# Upload a local file (streamed, not read fully into memory)
with open("local.tar.gz", "rb") as f:
    sandbox.filesystem.write_bytes("/tmp/archive.tar.gz", f)

# Read a file as text
text = sandbox.filesystem.read_text("/tmp/hello.txt")
print(text)  # "Hello, World!"

# Read a file as raw bytes
data = sandbox.filesystem.read_bytes("/tmp/data.bin")
```

Parent directories are created automatically on uploads.

## Sandbox management

```python
# List sandboxes
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
    allow_internet_egress=False,             # block outbound internet traffic (default)
    allowed_egress_cidrs=["104.16.0.0/12"],  # fine-grained CIDR allowlist
    allow_cluster_dns=False,                 # disable cluster DNS resolution (default)
    nameservers=["8.8.8.8"],                 # custom DNS nameservers
    allow_ipv6_egress=False,                 # extend egress config to IPv6
)
```

Cap outbound bandwidth with an egress rate limit:

```python
from isola import Network

sandbox = client.sandboxes.create(
    image="alpine:3.21",
    network=Network(
        allow_internet_egress=True,
        egress_rate_limit_bytes_per_second=10_000_000,  # 10 MB/s
    ),
)
```

## Rootfs snapshots

> Requires rootfs snapshots to be enabled and a storage bucket configured in your Helm values (`operator.sandboxRuntime.rootfssnapshot`).

Rootfs snapshots capture one container's root filesystem changes so you can restore them later in a new sandbox. This is useful for pre-warming environments: install dependencies once, snapshot, then spin up fresh sandboxes from that snapshot.

### Create a snapshot

```python
snapshot = client.rootfs_snapshots.create(
    sandbox_id=sandbox.id,
    snapshot_name="my-snapshot",
)
print(snapshot.status)  # RootfsSnapshotStatus.SUCCEEDED
```

`create()` blocks `max_wait_seconds` until the snapshot completes. Pass `max_wait_seconds=0` to return immediately.

### Restore from a snapshot

```python
restored = client.sandboxes.create(
    image="alpine:3.21",
    rootfs_snapshot_name="my-snapshot",
)
```

### Full round-trip example

```python
from isola import Isola, Network

client = Isola()

# 1. Install a heavy stack once, with internet connectivity
with client.sandboxes.create(
    image="python:3.12-slim",
    network=Network(allow_internet_egress=True),
    ephemeral_storage=4096,
) as sandbox:
    sandbox.commands.run("pip", "install", "numpy", "pandas", "scikit-learn")
    snapshot = client.rootfs_snapshots.create(
        sandbox_id=sandbox.id,
        snapshot_name="datascience-base",
    )

# 2. Restore from the snapshotted rootfs, packages already installed, no internet needed
with client.sandboxes.create(
    image="python:3.12-slim",
    ephemeral_storage=4096,
    rootfs_snapshot_name="datascience-base",
) as sandbox:
    result = sandbox.commands.run("python3", "-c", """
from sklearn.datasets import load_iris
from sklearn.ensemble import RandomForestClassifier
X, y = load_iris(return_X_y=True)
print(RandomForestClassifier(random_state=0).fit(X, y).score(X, y))
""")
    print(result.stdout)
```

### Automatic snapshots on termination

A sandbox can be configured to snapshot automatically as part of its termination policy:

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
from isola import Container, ResourceList, ResourceRequirements

limits = ResourceRequirements(limits=ResourceList(cpu="500m", memory="256Mi", ephemeral_storage="1Gi"))

sandbox = client.sandboxes.create(
    containers=[
        Container(
            name="app",
            image="python:3.12-slim",
            command=["python", "-m", "http.server", "8080"],
            resources=limits,
        ),
        Container(
            name="worker",
            image="alpine:3.21",
            resources=limits,
        ),
    ],
)
```

Set CPU, memory, and ephemeral storage limits on every container. gVisor runs a single sentry process inside the pod cgroup, which is where limits apply to the sandbox. Kubernetes sums container limits into the pod cgroup only when every container declares one, so a missing limit on any container produces surprising pod-level behavior on that dimension: unbounded for CPU and memory, too-low caps for ephemeral storage.

Target a specific container when running commands or writing files:

```python
result = sandbox.commands.run("wget", "-qO-", "http://127.0.0.1:8080", container="worker")
sandbox.filesystem.write_text("/tmp/data.txt", "hello", container="app")
```

## Error handling

API and SDK errors inherit from `IsolaError`:

```
IsolaError
├── APIError
│   ├── BadRequestError
│   ├── NotFoundError
│   ├── ConflictError
│   ├── ValidationError
│   ├── InternalError
│   └── BadGatewayError
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

The SDK automatically retries on transient errors.
