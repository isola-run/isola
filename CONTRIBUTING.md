# Contributing to Isola

Thank you for considering contributing to Isola. Whether it is a bug report, a feature idea, documentation improvement, or code, all contributions are welcome.

## Getting started

- Look at [open issues](https://github.com/isola-run/isola/issues) for something to work on. Issues labeled `good first issue` are a good starting point.
- For bug reports or feature requests, [open an issue](https://github.com/isola-run/isola/issues/new) first.
- For non-trivial changes, discuss your approach in an issue before investing significant time. This avoids duplicate work and ensures the change is welcome.
- Small fixes (typos, docs, obvious bugs) can go straight to a pull request.

If you have questions, start a [GitHub Discussion](https://github.com/isola-run/isola/discussions).

## Development setup

### Prerequisites

Install these before running the setup script:

- [Docker](https://docs.docker.com/get-docker/)
- [Go](https://go.dev/dl/) 1.21+ (the project uses Go 1.26, which is downloaded automatically via [GOTOOLCHAIN](https://go.dev/doc/toolchain))
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)
- [Tilt](https://docs.tilt.dev/install.html)
- [uv](https://docs.astral.sh/uv/getting-started/installation/) (for the Python SDK)
- [golangci-lint](https://golangci-lint.run/welcome/install/) (for linting and formatting)
- [setup-envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest) (for unit tests)

Optional:
- [controller-gen](https://book.kubebuilder.io/reference/controller-gen) -- needed only if you modify CRD types
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) -- vulnerability scanning
- [lefthook](https://github.com/evilmartians/lefthook) -- Git hooks

### One-time setup

Run the setup script to create a Kind cluster with a local registry and gVisor:

```bash
./hack/setup.sh
```

### Start developing

```bash
tilt up
```

Tilt builds all components, deploys them to the Kind cluster, and watches for file changes. The Tilt UI is at http://localhost:10350. The API gateway is port-forwarded to `localhost:8080`.

To tear down the cluster:

```bash
kind delete cluster --name isola-dev
```

## Making changes

1. Create a branch from `main`.
2. Make your changes.
3. Run checks and tests locally:
   ```bash
   make fix-all      # auto-fix formatting (Go + Python)
   make check-all    # all lints and checks
   make test          # unit tests
   ```
4. Commit with a clear, imperative message (e.g., "add snapshot TTL validation").
5. Open a pull request against `main`.

CI runs the same checks on every pull request. All checks must pass before merge.

### Generated files

Some files are generated and should not be edited by hand:

| After changing | Run |
|----------------|-----|
| `api/v1alpha1/*_types.go` (CRD types) | `make generate manifests` |
| Handler types or route registrations | `make openapi` |

CI verifies generated files are in sync.

## Code style

### Go

```bash
make fmt        # auto-format
make lint       # check
make lint-fix   # check and auto-fix
```

### Python SDK

The Python SDK is in `sdks/python/`, managed with uv:

```bash
make sdk-python-fmt        # auto-format
make sdk-python-lint       # check
make sdk-python-typecheck  # type checking
```

## Testing

### Unit tests

```bash
make test                          # all unit tests
make test-operator FOCUS="Name"    # focused operator tests (Ginkgo pattern)
make test-gateway                  # API gateway tests
make test-sidecar                  # sandbox sidecar tests
```

### Python SDK tests

```bash
make test-sdk-python
```

### E2E tests

E2E tests require a running cluster (`tilt up`):

```bash
make test-e2e                      # parallel E2E tests
make test-e2e-slow                 # include slow tests
```

Run `make help` for the full list of targets.

## Pull request process

- A maintainer will review your pull request. If you do not get a response within a few days, feel free to ping in the PR.
- Address review feedback in new commits rather than force-pushing, so the conversation stays readable.
- Use draft pull requests if you want early feedback on work in progress.
