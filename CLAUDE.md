# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is **isola** - a Kubernetes-based sandboxing platform with three Go microservices:

- **isola-operator** - Kubernetes operator managing Sandbox/SandboxTemplate/NetworkTemplate CRDs
- **isola-gw** - API gateway handling client requests and cloud storage integration
- **isola-agent** - Sidecar agent running tasks within sandbox pods

## Build Commands

### isola-operator (primary development target)
```bash
make build              # Build manager binary
make test               # Run unit tests with coverage
make test-verbose       # Verbose test output
make test-focus FOCUS="TestName"  # Run specific test
make lint               # Run golangci-lint
make lint-fix           # Auto-fix lint issues
make fmt                # Format code
make vet                # Run go vet
make manifests          # Generate CRDs and RBAC configs
make generate           # Generate DeepCopy methods
```

### Local Development
```bash
./deploy.sh             # Full deployment to local Minikube
```

## Architecture

```
services/
├── isola-operator/     # Go - Kubebuilder operator
│   ├── api/v1alpha1/   # CRD type definitions (Sandbox, SandboxTemplate, NetworkTemplate)
│   ├── internal/controller/  # Reconciliation logic
│   └── config/         # Kustomize deployment manifests
├── isola-gw/           # Go - Gin-based gateway
│   └── internal/       # handlers, kubernetes client, storage abstraction
└── isola-agent/        # Go - Gin-based agent sidecar deployed in the sandbox pods
    └── internal/handlers/

charts/                 # Helm charts for each component (source of truth for the whole project deployment)
tests/                  # Python pytest E2E tests
```

**Default Namespaces:**
- `isola-system` - operator and gateway run here
- `isola-sandboxes` - sandbox pods with agent sidecars run here

**Key dependencies:**
- controller-runtime for Kubernetes operator pattern
- Gin web framework for HTTP services
- gocloud.dev for cloud-agnostic blob storage
- Ginkgo/Gomega for Go BDD testing

## Testing

**Go tests** use Ginkgo/Gomega with envtest for Kubernetes API simulation.

**E2E tests** use Python pytest with fixtures in `tests/conftest.py`. Run against a deployed cluster with:
```bash
pytest tests/ -v -s --base-url=http://localhost:30080 --api-key=iso_sk_demo
```

or simply:

```bash
./scripts/run-e2e.sh    # Run Python E2E tests against deployed cluster
```

## CRD Development

When modifying CRD types in `api/v1alpha1/`:
1. Edit `*_types.go` files
2. Run `make generate` to regenerate DeepCopy methods
3. Run `make manifests` to regenerate CRD YAML

In order to sync the generated code from the operator to the helm charts:
```bash
 cd services/isola-operator && make generate manifests && cp config/crd/bases/*.yaml ../../charts/isola-operator/templates/crds/ && cp config/rbac/role.yaml ../../charts/isola-operator/templates/clusterrole.yaml
```
