---
sidebar_position: 1
title: Local Setup
---

# Local Development Setup

This guide walks through setting up a local Isola development environment.

## Prerequisites

Install the following tools:

| Tool | Purpose |
|------|---------|
| [Docker](https://docs.docker.com/get-docker/) | Container runtime |
| [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation) | Local Kubernetes clusters |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | Kubernetes CLI |
| [Helm](https://helm.sh/docs/intro/install/) | Package manager for Kubernetes |
| [Tilt](https://docs.tilt.dev/install.html) | Local development environment |

Optional tools for development:

| Tool | Purpose |
|------|---------|
| golangci-lint | Go linting |
| govulncheck | Go vulnerability scanning |
| controller-gen | CRD code generation |
| setup-envtest | Test environment setup |
| lefthook | Git hooks |

## One-Time Setup

Run the setup script to create a Kind cluster with gVisor, a local registry, and required resources:

```bash
./hack/setup.sh
```

This script:

1. Verifies required tools are installed
2. Creates a local Docker registry on `localhost:5001`
3. Creates a Kind cluster named `isola-dev`
4. Installs gVisor (`runsc`) in cluster nodes
5. Configures containerd to use the gVisor runtime
6. Creates a `gvisor` RuntimeClass
7. Sets up lefthook git hooks (if available)

## Start Development

After setup, start the development environment with Tilt:

```bash
tilt up
```

Open the Tilt UI at [http://localhost:10350](http://localhost:10350) to monitor builds and services.

Tilt watches for code changes and automatically rebuilds and redeploys the affected services.

## Building

Build all binaries locally:

```bash
make build
```

This produces:
- `bin/operator`
- `bin/api-gateway`
- `bin/sandbox-sidecar`
- `bin/uploader`

## Code Generation

After modifying CRD type definitions (`api/v1alpha1/*_types.go`):

```bash
make generate    # Regenerate DeepCopy methods
make manifests   # Regenerate CRD YAML and RBAC manifests
```

After modifying HTTP handler types or route registrations:

```bash
make openapi     # Regenerate OpenAPI specs
```

## Linting and Formatting

```bash
# Go
make lint          # Run golangci-lint
make lint-fix      # Auto-fix lint issues
make fmt           # Format code
make vet           # Run go vet
make vulncheck     # Check for known vulnerabilities

# Python SDK
make sdk-python-lint          # Lint with ruff
make sdk-python-fmt           # Format with ruff
make sdk-python-typecheck     # Type-check with mypy

# Everything
make check-all     # All checks (Go + Python SDK, CI-safe)
make fix-all       # Auto-fix all formatting and lint issues
```

## Teardown

Delete the Kind cluster when you're done:

```bash
kind delete cluster --name isola-dev
```
