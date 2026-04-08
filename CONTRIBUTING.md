# Contributing to Isola

Contributions are welcome. This guide covers the development setup and workflow.

## Prerequisites

Required:
- [Docker](https://docs.docker.com/get-docker/)
- [Go](https://go.dev/dl/) 1.26+
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)
- [Tilt](https://docs.tilt.dev/install.html)
- [uv](https://docs.astral.sh/uv/getting-started/installation/) (Python package manager)

Optional:
- [golangci-lint](https://golangci-lint.run/welcome/install/) - Go linting
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) - Go vulnerability scanning
- [controller-gen](https://book.kubebuilder.io/reference/controller-gen) - CRD/RBAC code generation
- [setup-envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest) - test environment binaries
- [lefthook](https://github.com/evilmartians/lefthook) - Git hooks

`hack/setup.sh` checks for all of these and prints install instructions for anything missing.

## Development setup

Run the one-time setup script. It creates a Kind cluster with a local registry, installs gVisor on the nodes, and configures the cluster for development:

```bash
./hack/setup.sh
```

Start the development environment:

```bash
tilt up
```

Tilt builds all components, deploys them to the Kind cluster, and watches for changes. The Tilt UI is available at http://localhost:10350.

## Making changes

1. Create a branch from `main`.
2. Make your changes.
3. Run checks locally before pushing:
   ```bash
   make check-all    # all lints and checks (Go + Python SDK)
   make test          # unit tests
   ```
4. Open a pull request against `main`.

### Generated files

Some files are generated and should not be edited by hand. After modifying source files, regenerate and commit the output:

| After changing | Run |
|----------------|-----|
| `api/v1alpha1/*_types.go` (CRD types) | `make generate manifests` |
| Handler types or route registrations | `make openapi` |

CI verifies generated files are in sync (`make check-openapi`, `make check-manifests`).

## Code style

### Go

Formatting and linting are handled by golangci-lint:

```bash
make fmt        # auto-format
make lint       # check
make lint-fix   # check and auto-fix
```

### Python SDK

The Python SDK lives in `sdks/python/` and is managed with uv. Formatting and linting use ruff, type checking uses mypy:

```bash
make sdk-python-fmt        # auto-format
make sdk-python-lint       # check
make sdk-python-lint-fix   # check and auto-fix
make sdk-python-typecheck  # type checking
```

Or run all checks at once:

```bash
make sdk-python-check-all
```

## Testing

### Unit tests

```bash
make test                          # all unit tests
make test-operator FOCUS="Name"    # focused operator tests
make test-gateway                  # API gateway tests
make test-sidecar                  # sandbox sidecar tests
```

### Python SDK tests

```bash
make test-sdk-python               # run tests
make test-sdk-python-verbose       # verbose output
```

### E2E tests

E2E tests require a running cluster (`tilt up`):

```bash
make test-e2e                      # parallel E2E tests
make test-e2e-slow                 # include slow tests
```

### All checks and tests

```bash
make check-all    # all checks, no tests
make fix-all      # auto-fix all formatting and lint issues
```

Run `make help` for the full list of targets.

## Project structure

```
api/v1alpha1/          CRD type definitions (Sandbox, RootfsSnapshot)
api/openapi/           Generated OpenAPI specs
charts/isola/          Helm chart (CRDs, templates, values)
cmd/                   Binary entry points (operator, api-gateway, sandbox-sidecar, snapshot-uploader)
internal/              Implementation (operator controllers, gateway handlers, sidecar)
hack/                  Development scripts
sdks/python/           Python SDK
tests/e2e/             End-to-end tests
```
