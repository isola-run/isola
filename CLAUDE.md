# CLAUDE.md

## Project Overview

This is **isola** - a Kubernetes-based sandboxing platform with three Go microservices:

- **isola-operator** - Kubernetes operator managing Sandbox/SandboxTemplate/NetworkTemplate CRDs
- **isola-gw** - API gateway handling client requests and cloud storage integration
- **isola-agent** - Sidecar agent running tasks within sandbox pods

This code is intended to be open sourced, and currently not yet release or in production, so breaking changes are OK.

## Build Commands

### Linting and Formatting
```bash
# From repo root (all services)
make check-all          # Run vet + lint + vulncheck (CI-safe, read-only)
make fix-all            # Auto-fix formatting and lint issues

# From any service directory (e.g., cd services/isola-gw)
make check              # Same as above, single service
make fix                # Same as above, single service
```

All services share `/.golangci.yml`. Run `make help` for full command list.

### isola-operator (primary development target)
```bash
make build              # Build manager binary
make test               # Run unit tests with coverage
make test-verbose       # Verbose test output
make test-focus FOCUS="TestName"  # Run specific test
make manifests          # Generate CRDs and RBAC configs
make generate           # Generate DeepCopy methods
```

### Local Development (Kind + Tilt)

**Quick Start:**
```bash
./scripts/setup.sh                   # One-time: creates Kind cluster + local registry
tilt up                              # Start dev environment (live-reload)
cd tests && uv run pytest -m smoke   # Run smoke tests
kind delete cluster --name isola-dev # Teardown
```

**Development Workflow:**
1. Run `./scripts/setup.sh` once to create the Kind cluster
2. Run `tilt up` to start development (web UI: http://localhost:10350)
3. Make code changes - Tilt automatically rebuilds and redeploys
4. API is available at http://localhost:30080
5. Run tests: `cd tests && uv run pytest -m smoke`

**Pre-commit Hooks (optional):**
Lefthook auto-lints only affected services on commit. Install with:
```bash
go install github.com/evilmartians/lefthook/v2@v2.0.13 && lefthook install
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

**Go unit tests** use Ginkgo/Gomega with envtest for Kubernetes API simulation:
```bash
cd services/isola-operator && make test
```

**E2E tests** use Python pytest (run from `tests/` directory):
```bash
cd tests
uv run pytest                    # Run all E2E tests
uv run pytest -m smoke           # Quick smoke tests (~30s)
uv run pytest -k "lifecycle"     # Run specific test pattern
uv run pytest --skip-cleanup     # Keep sandboxes for debugging
```

**Environment variables:**
- `ISOLA_BASE_URL` - isola-gw URL (default: `http://localhost:30080`)
- `ISOLA_API_KEY` - API key (default: `iso_sk_demo`)

**E2E Test Structure:**
```
tests/
├── conftest.py              # Fixtures: isola_client, sandbox, wait_for_isola_gw
├── client/isola_client.py   # API client wrapper (pre-SDK)
├── test_sandbox_lifecycle.py    # Create, get, list, terminate
├── test_command_execution.py    # Execute commands in sandboxes
├── test_error_handling.py       # 401, 404, validation errors
└── test_file_operations.py      # File upload/download
```

**Test Markers:**
- `@pytest.mark.smoke` - Quick sanity tests
- `@pytest.mark.slow` - Tests taking >30s
- `@pytest.mark.network` - Network isolation tests

## CRD Development

When modifying CRD types in `api/v1alpha1/`:
1. Edit `*_types.go` files
2. Run `make generate` to regenerate DeepCopy methods
3. Run `make manifests` to regenerate CRD YAML

In order to sync the generated code from the operator to the helm charts:
```bash
cd services/isola-operator && make generate manifests && cp config/crd/bases/*.yaml ../../charts/isola-operator/crds/ && cp config/rbac/role.yaml ../../charts/isola-operator/templates/clusterrole.yaml
```

## Code Comments

Only comment on non-obvious code segments. Avoid comments that simply restate what the code does when it's already clear from the function/variable names.

**Bad examples:**
```go
// Check if job failed
if isJobFailed(job) {

// Job is still running
return ...
```

**Good examples:**
```go
// RunAsUser 0 (root) is needed to read /proc/<pid>/environ of other users' processes
// and to access /proc/<pid>/root when using shared PID namespace.
SecurityContext: &corev1.SecurityContext{
    RunAsUser: ptr.To(int64(0)),
}
```

If code needs a comment to be understood, first consider if better naming or restructuring could make it self-explanatory.

