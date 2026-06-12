<p align="center">
  <img src="static/isola-logo.svg" alt="Isola" width="450" />
</p>

<p align="center">
  <strong>Secure sandboxing for Kubernetes</strong>
</p>

<p align="center">
  <a href="https://github.com/isola-run/isola/blob/main/LICENSE"><img src="https://img.shields.io/github/license/isola-run/isola" alt="License" /></a>
  <a href="https://github.com/isola-run/isola/actions"><img src="https://github.com/isola-run/isola/actions/workflows/test.yml/badge.svg" alt="CI" /></a>
</p>

---

Isola is an open-source platform for running untrusted and AI-generated code securely on your own Kubernetes cluster. It uses [gVisor](https://gvisor.dev) to isolate each pod behind its own application kernel.

Create sandboxes from any OCI image, execute commands, stream output, read and write files, and snapshot the root filesystem for reuse. Isola provides a REST API and SDKs for building AI agents, code interpreters, and other applications that need to run untrusted code safely. It is self-hosted and operates with the tools you already know.

## Quick start

These examples assume a running Isola cluster. See [Deployment](#deployment) for production setup, or run `hack/setup.sh` to get a local development cluster with Kind.

Install the SDK for your language:

```bash
pip install isola-run        # Python 3.10+
npm install @isola-run/sdk   # Node 22+
```

Create a sandbox, run a command, and read files:

```python
from isola import Isola

with Isola(url="http://localhost:8080") as client:  # or set ISOLA_URL env var
    sandbox = client.sandboxes.create(image="python:3.12-slim")

    result = sandbox.commands.run("python3", "-c", "print('hello from the sandbox')")
    print(result.stdout)    # "hello from the sandbox\n"

    sandbox.filesystem.write("/tmp/hello.txt", "Hello, World!")
    data = sandbox.filesystem.read("/tmp/hello.txt")
    print(data.decode())    # "Hello, World!"

    sandbox.delete()
```

The same workflow in TypeScript:

```ts
import { Isola } from "@isola-run/sdk";

const client = new Isola({ url: "http://localhost:8080" }); // or set ISOLA_URL env var

const sandbox = await client.sandboxes.create({ image: "python:3.12-slim" });

const result = await sandbox.commands.run(["python3", "-c", "print('hello from the sandbox')"]);
console.log(result.stdout); // "hello from the sandbox\n"

await sandbox.filesystem.write("/tmp/hello.txt", "Hello, World!");
const data = await sandbox.filesystem.read("/tmp/hello.txt");
console.log(new TextDecoder().decode(data)); // "Hello, World!"

await sandbox.delete();
```

Snapshot the filesystem and restore it in new sandboxes. Pre-warm an environment once and reuse it, or snapshot a working session and pick it up later:

```python
from isola import Isola, Network

with Isola(url="http://localhost:8080") as client:
    sandbox = client.sandboxes.create(
        image="python:3.12-slim",
        network=Network(allow_internet_egress=True),  # enable internet for setup
    )
    sandbox.commands.run("pip", "install", "numpy", "pandas")
    client.rootfs_snapshots.create(sandbox_id=sandbox.id, snapshot_name="datascience")
    sandbox.delete()

    # Every new sandbox starts with packages pre-installed, no internet needed
    sandbox = client.sandboxes.create(
        image="python:3.12-slim",
        rootfs_snapshot_name="datascience",
    )
    result = sandbox.commands.run("python3", "-c", "import pandas; print(pandas.__version__)")
    print(result.stdout)
    sandbox.delete()
```

See the [Python SDK](sdks/python/README.md) and [TypeScript SDK](sdks/typescript/README.md) documentation for the full API reference, and the [OpenAPI spec](api/openapi/api-gateway.yaml) for the REST API.

## Why Isola?

- **Open source.** Apache 2.0 licensed. The code is yours to audit, modify, and deploy.

- **Self-hosted.** Sandboxes run on your Kubernetes nodes. You control your infrastructure and your data.

- **Kubernetes-native.** Sandboxes and snapshots are Custom Resources managed by a Kubernetes operator. They integrate naturally with your existing RBAC, observability, and cluster operations.

- **Simple to operate.** One Helm install. No database, no Redis, no message queue. The only dependencies are a Kubernetes cluster with gVisor and an optional object storage bucket for snapshots. Use any OCI container image as a sandbox base, no custom templates or build steps required.

- **gVisor isolation.** [gVisor](https://gvisor.dev) intercepts application system calls in user space, providing a strong security boundary without requiring hardware virtualization.

- **Rootfs snapshot and restore.** Capture a sandbox's filesystem changes and restore them in a new sandbox on any node. Pre-warm environments or persist working sessions across sandbox lifetimes.

- **Configurable network isolation.** Sandboxes have no network access by default. Enable internet egress or restrict traffic to specific CIDRs, per sandbox.

- **Language SDKs.** Python SDK with sync and async clients, and a TypeScript SDK for Node, Bun, Deno, and edge runtimes. Any language can use the [REST API](api/openapi/api-gateway.yaml) directly.


## What Isola is not

- **Not a hosted service.** There is no SaaS offering. You deploy and operate Isola on your own Kubernetes cluster.
- **Not a VM-based sandbox.** Isola uses gVisor for isolation, not virtual machines like Firecracker. No KVM or bare-metal machines required.

## Features

### Sandbox lifecycle

Create sandboxes from OCI container images with configurable CPU, memory, and ephemeral storage limits. Set a timeout for automatic cleanup, or delete sandboxes explicitly.

```python
sandbox = client.sandboxes.create(
    image="python:3.12-slim",
    cpu=0.5,                  # CPU cores
    memory=256,               # MiB
    ephemeral_storage=1024,   # MiB
    timeout_seconds=3600,     # auto-delete after 1 hour
)
```

### File I/O

Read and write files inside sandboxes. Parent directories are created automatically.

```python
sandbox.filesystem.write("/app/main.py", "print('hello from the sandbox')")
data = sandbox.filesystem.read("/app/main.py")
print(data.decode())  # "print('hello from the sandbox')"
```

### Command execution

Run commands and wait for completion, or spawn non-blocking commands and stream stdout/stderr as output arrives. Send input to stdin for interactive processes. Useful for AI agents that need to execute code, run shell commands, or drive interactive tools.

```python
# Run the script we just uploaded
result = sandbox.commands.run("python3", "/app/main.py")
print(result.stdout)  # "hello from the sandbox\n"

# Non-blocking with streaming
cmd = sandbox.commands.spawn("python3", "/app/main.py")
for chunk in cmd.stdout:
    print(chunk, end="")

# Stdin
result = sandbox.commands.run("python3", input="print('hello from stdin')\n")
print(result.stdout)  # "hello from stdin\n"
```

### Rootfs snapshots

Capture a container's root filesystem changes to cloud storage (S3, GCS, or Azure Blob Storage) and restore them in new sandboxes on any node. Only the modified overlay layer is captured, not the full image, making it efficient to set up an environment once and reuse it, or preserve sandbox state between sessions.

Snapshots are stored in a cloud storage bucket and made available on every node automatically. Restore is resilient: when a sandbox references a snapshot, it retries until the snapshot appears or the startup timeout expires.

Sandboxes can also be configured to snapshot automatically as part of their termination policy:

```python
from isola import SnapshotRootfs

sandbox = client.sandboxes.create(
    image="alpine:3.21",
    termination_policy=SnapshotRootfs(snapshot_name="on-exit-snapshot"),
)
```

The policy runs when the operator tears the sandbox down: `sandbox.delete()`, `timeout_seconds` expiring, or the `Sandbox` resource being deleted directly. It does *not* run when the container exits on its own (e.g. crash or entrypoint completed in case of a custom entrypoint).

### Network isolation

Sandboxes are isolated by default with a deny-all network policy. Network access is configured per sandbox:

```python
from isola import Network

# Full internet access
sandbox = client.sandboxes.create(
    image="alpine:3.21",
    network=Network(allow_internet_egress=True),
)

# Restricted to specific CIDRs only
sandbox = client.sandboxes.create(
    image="alpine:3.21",
    network=Network(allowed_egress_cidrs=["104.16.0.0/12"]),
)
```

Private IP ranges and cloud metadata endpoints are blocked automatically when internet egress is enabled.

Network isolation relies on Kubernetes [NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/) and requires a CNI that enforces it. Most managed Kubernetes services (EKS, AKS, GKE) support this natively or through built-in options.

### Multi-container sandboxes

Run multiple containers in a single sandbox. Containers share a network namespace and can communicate over localhost. Useful for running an application alongside its dependencies (databases, MCPs, tool servers) in one isolated environment:

```python
from isola import Container, ResourceList, ResourceRequirements

limits = ResourceRequirements(limits=ResourceList(cpu="500m", memory="256Mi", ephemeral_storage="1Gi"))

sandbox = client.sandboxes.create(
    containers=[
        Container(name="api", image="python:3.12-slim", command=["python3", "-m", "http.server", "8080"], resources=limits),
        Container(name="test", image="alpine:3.21", resources=limits),
    ],
)
result = sandbox.commands.run("wget", "-qO-", "http://127.0.0.1:8080", container="test")
```

Set CPU, memory, and ephemeral storage limits on every container. gVisor runs a single sentry process inside the pod cgroup, which is where limits apply to the sandbox. Kubernetes sums container limits into the pod cgroup only when every container declares one, so a missing limit on any container produces surprising pod-level behavior on that dimension: unbounded for CPU and memory, too-low caps for ephemeral storage.

## Architecture

| Component | Role |
|-----------|------|
| **Operator** | Watches Sandbox and RootfsSnapshot custom resources. Reconciles the desired state into pods, network policies, and snapshot jobs. |
| **API Gateway** | Exposes the public REST API. Proxies command and file operations to the sandbox sidecar. Translates lifecycle requests into Kubernetes custom resources. |
| **Sandbox Sidecar** | Runs in every sandbox pod. Handles command execution (spawn, stream, kill) and filesystem operations on behalf of the API gateway. |
| **Snapshot Mounter** | A DaemonSet that runs an NFS server backed by cloud storage (via rclone) on each node. Makes snapshot tars available to gVisor for rootfs restore. |
| **Snapshot Uploader** | A short-lived Job created per snapshot. Reads the gVisor overlay upper layer and uploads it to the configured storage bucket. |
| **Helm chart** | Ties the system together. Installs CRDs, deploys the operator and gateway, configures RBAC and network policies, and optionally sets up the snapshot infrastructure. |

## Deployment

### Prerequisites

- A Kubernetes cluster (vanilla, EKS, AKS, GKE, or similar).
- [Helm](https://helm.sh).
- A [gVisor](https://gvisor.dev) RuntimeClass configured in your cluster — or let the chart install it for you with `--set gvisor.autoInstall.enabled=true` (see [gVisor setup](#gvisor-setup) below).
- (Optional) An S3, GCS, or Azure Blob Storage bucket for rootfs snapshots.

### Install with Helm

Sandbox pods run in a separate namespace from the control plane (`isola-sandboxes` by default, configurable via `sandboxNamespace.name`). Create it before installing:

```bash
kubectl create namespace isola-sandboxes
helm install isola oci://ghcr.io/isola-run/charts/isola --namespace isola-system --create-namespace
```

Alternatively, set `sandboxNamespace.create: true` in your Helm values to let the chart create the namespace. Note that `helm uninstall` will then cascade-delete the namespace and all sandboxes inside it.

The API gateway service is cluster-internal by default. For quick access, use port-forwarding:

```bash
kubectl port-forward -n isola-system svc/isola-api-gateway 8080:80
export ISOLA_URL=http://localhost:8080
```

For local development with Kind, `hack/setup.sh` automates the full cluster setup.

### Upgrading

```bash
helm upgrade isola oci://ghcr.io/isola-run/charts/isola --namespace isola-system
```

CRDs are upgraded automatically as part of the Helm chart. To manage CRDs externally, install with `--set crds.enabled=false`.

### gVisor setup

Isola requires a gVisor [RuntimeClass](https://kubernetes.io/docs/concepts/containers/runtime-class/) named `gvisor` in your cluster. There are two ways to get one.

#### Option A: automatic installation (recommended)

The chart can install and manage gVisor on your nodes for you:

```bash
helm install isola oci://ghcr.io/isola-run/charts/isola \
  --namespace isola-system --create-namespace \
  --set gvisor.autoInstall.enabled=true
```

This deploys a privileged DaemonSet that downloads the pinned gVisor release (sha512-verified) onto each node, registers the `runsc` containerd handler, restarts containerd once (running containers are unaffected; every change is validated first and rolled back automatically if containerd does not come back healthy), creates the RuntimeClass, and labels each node `isola.run/gvisor=true` once the runtime is verified. Sandboxes only schedule onto labeled nodes. Nodes that already have a `runsc` handler are detected and left untouched.

It requires nodes with a standard containerd setup (config at `/etc/containerd/config.toml`, systemd) — kind, kubeadm, EKS, AKS, and GKE Ubuntu nodes qualify; k3s/RKE2/MicroK8s/k0s are detected and skipped with a clear node event. See the `gvisor.autoInstall` section of `values.yaml` for version pinning, upgrades, mirrors, per-node opt-out, and cleanup notes.

#### Option B: manual installation

If you prefer to manage gVisor yourself:

0. Ensure your cluster uses [containerd](https://containerd.io/docs/2.2/getting-started/) (the default container runtime on EKS, AKS, GKE and most clusters).

1. Install the `runsc` binary and `containerd-shim-runsc-v1` on each node. See the [gVisor quickstart](https://gvisor.dev/docs/user_guide/containerd/quick_start/) for instructions. If you plan to use rootfs snapshots, install [`release-20260126.0`](https://storage.googleapis.com/gvisor/releases/release/20260126/x86_64/runsc) or later.

2. Configure the containerd runtime. Create `/etc/containerd/runsc.toml`:

```toml
[runsc_config]
  allow-rootfs-tar-annotation = "true"
  systemd-cgroup = "true"
```

Add the runtime to your containerd config (typically `/etc/containerd/config.toml`):

```toml
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
  pod_annotations = ["dev.gvisor.*"]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc.options]
  TypeUrl = "io.containerd.runsc.v1.options"
  ConfigPath = "/etc/containerd/runsc.toml"
```

See the [gVisor containerd configuration guide](https://gvisor.dev/docs/user_guide/containerd/configuration/) for details. The `pod_annotations` allowlist is required for Isola's gVisor annotations to pass through, and `allow-rootfs-tar-annotation` enables rootfs snapshot support.

3. Restart containerd and create the RuntimeClass:

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
```

For local development, `hack/setup.sh` automates all of the above in a Kind cluster.

### Rootfs snapshots (optional)

Rootfs snapshots are disabled by default. To enable them, configure a storage bucket in your Helm values:

```yaml
operator:
  sandboxRuntime:
    rootfssnapshot:
      enabled: true
      storage:
        type: s3   # or "gcs" or "azure"
        s3:
          bucket: my-isola-snapshots
          region: us-east-1
```

Credentials can be provided via workload identity (recommended), a pre-existing Kubernetes secret, or inline values. See [values.yaml](charts/isola/values.yaml) for all options.

## Documentation

| Resource | Description |
|----------|-------------|
| [Python SDK](sdks/python/README.md) | Full SDK reference: sync/async clients, sandbox options, commands, files, snapshots, error handling |
| [TypeScript SDK](sdks/typescript/README.md) | Full SDK reference for Node, Bun, Deno, and edge runtimes: sandboxes, commands, files, snapshots, streaming, error handling |
| [API spec](api/openapi/api-gateway.yaml) | OpenAPI specification for the REST API |
| [Helm values](charts/isola/values.yaml) | All configuration options for the Isola Helm chart |
| [isola.run](https://isola.run) | Project website |

## Getting help

- Open an [issue](https://github.com/isola-run/isola/issues) to report bugs or request features.
- Start a [discussion](https://github.com/isola-run/isola/discussions) for questions and general conversation.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the full guide. The quickest path to a local dev environment:

```bash
./hack/setup.sh   # one-time: Kind cluster, local registry, gVisor
tilt up            # start the dev environment
make test          # run unit tests
```

Run `make help` for all available targets.

## Security

To report a security vulnerability, see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0. See [LICENSE](LICENSE) for the full license text.
