<p align="center">
  <img src="docs/isola-logo.svg" alt="Isola" width="160" />
</p>

<h1 align="center">Isola</h1>

<p align="center">
  Secure sandbox orchestration for Kubernetes
</p>

<p align="center">
  <a href="https://github.com/isola-run/isola/actions/workflows/test.yml"><img src="https://github.com/isola-run/isola/actions/workflows/test.yml/badge.svg" alt="Tests"></a>
  <a href="https://github.com/isola-run/isola/actions/workflows/e2e.yml"><img src="https://github.com/isola-run/isola/actions/workflows/e2e.yml/badge.svg" alt="E2E"></a>
  <a href="https://github.com/isola-run/isola/actions/workflows/lint.yml"><img src="https://github.com/isola-run/isola/actions/workflows/lint.yml/badge.svg" alt="Lint"></a>
  <a href="https://github.com/isola-run/isola/blob/main/LICENSE"><img src="https://img.shields.io/github/license/isola-run/isola" alt="License"></a>
  <a href="https://github.com/isola-run/isola/releases"><img src="https://img.shields.io/github/v/release/isola-run/isola" alt="Release"></a>
</p>

---

Isola is a Kubernetes-native platform for running secure, isolated sandboxes. It combines [gVisor](https://gvisor.dev/) for OS-level isolation with a Kubernetes operator for declarative lifecycle management, a REST API gateway, and a Python SDK for programmatic access.

## Key Features

- **Secure isolation** -- Sandboxes run on [gVisor](https://gvisor.dev/), intercepting syscalls in userspace to minimize kernel attack surface
- **Declarative management** -- Kubernetes CRDs (`Sandbox`, `RootfsSnapshot`) for native integration with the K8s ecosystem
- **Rootfs snapshots** -- Capture and restore sandbox filesystem state via gVisor's overlay mechanism
- **Network policies** -- Fine-grained egress control: internet access, cluster DNS, custom CIDRs, custom nameservers
- **Python SDK** -- Sync and async clients with streaming command I/O, file operations, and snapshot management
- **REST API** -- OpenAPI-documented gateway for language-agnostic integration

## Architecture

```
                         ┌──────────────┐
                         │   Operator   │
                         │  (controller)│
                         └──────┬───────┘
                                │ watches/reconciles
                                │
                      K8s API Server
                       ┌────────┴────────┐
                       │                 │
               ┌───────▼──────┐  ┌───────▼───────────────┐
               │  API Gateway │  │     Sandbox Pods       │
               │   (REST)     │  │  ┌─────────────────┐   │
               │   :8080      ├──►  │    Sidecar       │   │
               └──────────────┘  │  │  (exec, file IO) │   │
                                 │  └─────────────────┘   │
  Python SDK ──► HTTP ──────────►│     gVisor runtime     │
                                 └────────────────────────┘
```

The **operator** manages sandbox lifecycle, network policies, and snapshot jobs. The **API gateway** provides a thin REST layer that proxies commands and file operations to the **sidecar** running inside each sandbox pod. The **Python SDK** wraps the REST API with high-level abstractions.

## Quick Start

### Install the Python SDK

```bash
pip install isola
```

### Create and use a sandbox

```python
from isola import Isola

client = Isola()

with client.sandboxes.create(image="alpine:3.21") as sandbox:
    result = sandbox.commands.run("echo", "hello world")
    print(result.stdout)      # "hello world\n"
    print(result.exit_code)   # 0

    sandbox.filesystem.write("/tmp/hello.txt", "hello")
    data = sandbox.filesystem.read("/tmp/hello.txt")
```

Sandboxes are context managers -- they are automatically deleted on exit.

### Streaming and non-blocking commands

```python
cmd = sandbox.commands.spawn("sh", "-c", "for i in 1 2 3; do echo $i; sleep 1; done")
for chunk in cmd.stdout:
    print(chunk, end="")
cmd.wait()
```

### Async support

```python
from isola import AsyncIsola

async with AsyncIsola() as client:
    async with await client.sandboxes.create(image="alpine:3.21") as sandbox:
        result = await sandbox.commands.run("echo", "hello")
        print(result.stdout)
```

### Sandbox options

```python
from isola import NetworkSpec

sandbox = client.sandboxes.create(
    image="python:3.12",
    command=["python", "-m", "http.server"],
    env={"PORT": "8080"},
    cpu="500m",
    memory="256Mi",
    ephemeral_storage="1Gi",
    timeout_seconds=3600,
    network=NetworkSpec(
        allow_internet_egress=True,
    ),
)
```

### Rootfs snapshots

Capture and restore sandbox filesystem state:

```python
snapshot = client.rootfs_snapshots.create(
    sandbox_id=sandbox.sandbox_id,
    snapshot_name="my-snapshot",
    max_wait_seconds=300,
)

restored = client.sandboxes.create(
    image="alpine:3.21",
    rootfs_snapshot_source="my-snapshot",
)
```

## Deployment

### Prerequisites

- Kubernetes 1.25+
- gVisor runtime installed on nodes
- Kubernetes 1.34+ for rootfs snapshot restore ([ContainerRestartRules](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) feature gate)

### Helm

```bash
helm install isola charts/isola \
  --namespace isola-system \
  --create-namespace
```

See [`charts/isola/values.yaml`](charts/isola/values.yaml) for configuration options.

## Local Development

```bash
# One-time setup: Kind cluster with gVisor + local registry
./hack/setup.sh

# Start dev environment
tilt up
# Dashboard at http://localhost:10350
```

See [`CLAUDE.md`](CLAUDE.md) for the full development guide, including `make` targets for testing, linting, and code generation.

## Project Structure

```
api/v1alpha1/           Kubernetes CRD type definitions
cmd/
  operator/             Sandbox lifecycle controller
  api-gateway/          REST API server
  sandbox-sidecar/      In-pod agent for exec and file I/O
  uploader/             Snapshot upload to S3-compatible storage
  snapshot-mounter/     NFS mount for snapshot restore
internal/               Implementation packages
sdks/python/            Python SDK
charts/isola/           Helm chart
tests/e2e/              End-to-end test suite
```

## Custom Resources

### Sandbox

Declares a sandboxed workload with resource limits, network policies, and optional snapshot restoration:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
spec:
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: alpine:3.21
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
  timeoutSeconds: 3600
  network:
    allowInternetEgress: true
```

### RootfsSnapshot

Captures the overlay filesystem of a running sandbox:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: RootfsSnapshot
spec:
  sandboxName: my-sandbox
  snapshotName: my-snapshot
  timeoutSeconds: 300
```

## Contributing

Contributions are welcome. Please open an issue to discuss your proposed changes before submitting a pull request.

```bash
make check-all    # Run all checks (lint, vet, formatting)
make test         # Run unit tests
make test-e2e     # Run end-to-end tests (requires tilt up)
```

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
