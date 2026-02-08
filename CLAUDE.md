# CLAUDE.md

## Commands

```bash
# Lint/format (from repo root)
make check-all          # vet + lint + vulncheck (CI-safe)
make fix-all            # Auto-fix formatting and lint issues

# Testing
make test               # Unit tests with coverage
make test-operator FOCUS="TestName"  # Run focused operator test
make test-gateway           # Run api-gateway tests
make generate           # Regenerate DeepCopy methods after CRD changes
make manifests          # Regenerate CRD YAML after CRD changes

# Build
make build              # Build all binaries to bin/

# Local dev
./hack/setup.sh         # One-time: Kind cluster + registry
tilt up                 # Start dev environment (http://localhost:10350)
# Make sure to keep the Tiltfile updated on changes to the cluster
```

## Critical Rules

**NEVER GUESS technical details.** This includes but is not limited to:
- Version numbers or version tags (e.g., package versions, tool versions, API versions)
- Package import paths or module names
- API endpoints, URLs, or connection strings
- Configuration file formats or schema details
- Command-line flags or arguments

If you don't know something:
1. **Search/verify first** - Use WebSearch, WebFetch, or Read to find accurate information
2. **Ask the user** - If you can't verify, ask rather than assume
3. **Test it** - When possible, try the command/code to verify it works

Never make assumptions about version compatibility, release tag formats, or tool behavior. Always verify.

## CRD Workflow

After modifying `api/v1alpha1/*_types.go`:
```bash
make generate manifests
```

This generates CRDs and RBAC directly to the Helm chart:
- CRDs → `charts/isola/crds/`
- RBAC → `charts/isola/generated/role.yaml`

The Helm `clusterrole.yaml` template uses `.Files.Get` to include the generated RBAC rules
with proper Helm templating for name/labels.

## OpenAPI Workflow

After modifying handler input/output types or route registrations:
```bash
make openapi  # Regenerates api/openapi/*.yaml from Huma type definitions
```

OpenAPI 3.1 specs are generated per service:
- `api/openapi/api-gateway.yaml` - End-user facing API
- `api/openapi/sandbox-sidecar.yaml` - Internal API (api-gateway → sidecar)

At runtime, each service also serves:
- `/docs` - Interactive Stoplight Elements UI
- `/openapi.json` - OpenAPI 3.1 spec (JSON)
- `/openapi.yaml` - OpenAPI 3.1 spec (YAML)

CI runs `make check-openapi` to verify generated specs are in sync.

## Architecture Notes

**Backward compatibility:** not required at this stage. You may introduce breaking changes to CRDs, APIs, and internal interfaces if it simplifies the design, provided tests are updated accordingly.

**Single Go module:** The project uses a single `go.mod` at the root (`github.com/isola-ai/isola-sb`). All binaries import from this module.

**Project structure:**
- `api/v1alpha1/` - CRD type definitions (Sandbox, RootfsSnapshot)
- `api/openapi/` - Generated OpenAPI specs (api-gateway.yaml, sandbox-sidecar.yaml)
- `cmd/operator/` - Kubebuilder operator entry point
- `cmd/api-gateway/` - API gateway for external clients
- `cmd/sandbox-sidecar/` - Sidecar injected into sandbox pods by operator
- `cmd/uploader/` - Snapshot uploader job (uploads tarballs to S3/GCS/Azure)
- `cmd/openapi-gen/` - CLI tool to generate OpenAPI specs from Huma types
- `internal/operator/controller/` - Reconciler implementations
- `internal/api-gateway/handlers/` - REST handlers, Huma route registrations, REST↔CRD conversion
- `internal/sandbox-sidecar/handlers/` - Sidecar handlers and route registrations
- `internal/snapshot/` - Shared snapshot types (used by operator and uploader)
- `charts/` - Helm charts (source of truth for deployment)
- `charts/isola/generated/` - Auto-generated RBAC from kubebuilder annotations (do not edit)

**Default namespaces:** `isola-system` (operator), `isola-sandboxes` (sandbox pods)

**Helm charts are source of truth** - CRDs and RBAC are generated directly to Helm chart via `make manifests`

## CRDs

**Sandbox** - A running sandbox instance. Key spec fields:
- `podTemplate` (required) - Inlined pod template with containers, volumes, etc.
- `activeDeadlineSeconds` - Max lifetime; operator calculates `status.timeoutAt` from pod start time
- `shutdownPolicy` - ShutdownPolicy struct with `strategy` (`Delete` default, or `SnapshotRootfs`) and optional `activeDeadlineSeconds` for the snapshot job
- `network` - NetworkSpec for isolation rules (immutable after creation)

**RootfsSnapshot** - Triggers a snapshot of a sandbox's filesystem. Creates an uploader Job that tarballs the container rootfs and uploads to cloud storage. Supports TTL-based auto-deletion.

## REST API (api-gateway)

The api-gateway is a thin passthrough to K8s — it validates input structure but does not apply domain defaults (that's the operator's job). Uses Huma framework on chi router.

**Endpoints:**
- `POST /sandboxes` → 201 (created), 409 (conflict), 400/422 (validation)
- `GET /sandboxes` → 200 (list of summaries — not paginated)
- `GET /sandboxes/{id}` → 200, 404
- `DELETE /sandboxes/{id}` → 204 (idempotent: both success and not-found)

**REST ↔ CRD conversion (`convert.go`):**
REST types are separate from CRD types with explicit conversion in `convert.go`. Key behaviors:
- `requestToSandboxCR` generates a 22-char alphanumeric name (starts with letter, DNS-1123 safe)
- Resource quantity strings (e.g. "125m", "512Mi") are parsed via K8s `resource.ParseQuantity` — failures return 400 with the field name
- Env var map keys are sorted for deterministic CRD output
- `sandboxToResponse` reads only the first container (single-container model for now)
- `conditionsToStatus` maps K8s condition Reasons to user-facing status enum: `creating`, `running`, `shuttingDown`, `failed`, `stopped`, `unknown`

**Env vars are write-only:** Request types accept env vars but response types intentionally omit them to avoid leaking secrets. `ContainerSpec` (request) has `Env`; `ContainerInfo` (response) does not.

**Default command:** Containers with no `command` get `["sleep", "infinity"]` injected by the operator so sandboxes stay alive by default. The gateway passes `command` through as-is (nil when omitted); the operator applies the default at pod creation time.

## Sharp Edges

**Two-client pattern in operator tests:** `suite_test.go` uses both `k8sClient` (direct, no cache delay for test writes) and `k8sCache` (cached, required for field index queries). Use the direct client for test assertions. The api-gateway tests use a single cached client from the manager.

**Conditions, not phases:** Sandbox status uses K8s conditions (`Ready`, `PodReady`, `NetworkConfigured`), not the deprecated phase pattern.

**Network isolation:**
- Default: deny-all egress with sink DNS (127.0.0.1) so DNS queries fail fast
- Custom rules via `Sandbox.spec.network` (NetworkSpec):
  - `allowAllInternet: true` - allows 0.0.0.0/0 egress (private ranges auto-blocked)
  - `allowedEgressCIDRs` - specific CIDR allowlist
  - `nameservers` - custom DNS servers (default: sink or 8.8.8.8/1.1.1.1 for internet)
- Network config is **immutable** after sandbox creation
- Static NetworkPolicies deployed via Helm handle base isolation
- Custom per-sandbox NetworkPolicies created by operator when NetworkSpec is set
- `allowAllInternet` and `allowClusterDNS` are `*bool` (pointer) — custom policy only created when CIDRs or private nameservers are specified

**Finalizers:** `sandbox.isola.run/cleanup` ensures cleanup before sandbox deletion.


**Snapshot workflow:**
1. Create RootfsSnapshot CR referencing a sandbox
2. Operator creates a Job with uploader container
3. Uploader tarballs the sandbox container's rootfs via `/proc/<pid>/root`
4. Uploads to cloud storage (S3/GCS/Azure via gocloud.dev)
5. Writes result to termination log, operator reads and updates status
6. TTL controller deletes snapshot after `ttlSecondsAfterCompletion`

**Dockerfiles copy all of internal/:** Each binary's Dockerfile copies the entire `internal/` directory rather than individual packages. This avoids needing to update Dockerfiles and Tiltfile when adding new `internal/` packages.

## Testing

**Go tests:** Ginkgo/Gomega with envtest (K8s API simulation)

```bash
make test                            # Run all tests
make test-verbose                    # All tests with verbose output
make test-operator                   # Operator tests only
make test-gateway                    # api-gateway tests only
make test-operator FOCUS="Reconcile" # Focused by Ginkgo pattern
make test GO_TEST_FLAGS="-race"      # With race detector
```

**Variables** (following Cluster API / Kubernetes patterns):
- `FOCUS` - Ginkgo focus pattern for component targets
- `SKIP` - Ginkgo skip pattern
- `GO_TEST_FLAGS` - Additional go test flags

**Operator tests** use a `FakeClock` (internal `Clock` interface) for deterministic timeout and snapshot testing — no flaky time.Sleep waits. Test fixtures in `internal/testutil/utils/fixtures.go` use functional options (`WithSandboxActiveDeadline`, `WithNetworkSpec`, `WithInternetAccess`, etc.).

**API gateway tests** use `humatest.TestAPI` for HTTP request/response testing against a real envtest K8s backend. Tests use `Eventually()` for cache eventual consistency. Error injection tests use controller-runtime's `interceptor.Funcs` to inject fake K8s API errors.

## Tooling Versions

Tool versions are pinned and must be kept in sync:

| Tool | Location | Also sync with |
|------|----------|----------------|
| Go | `Makefile` (`GO_VERSION`), `go.mod` | Dockerfile `FROM golang:` tags |
| golangci-lint | `hack/setup.sh` | `.github/workflows/lint.yml` |
| govulncheck | `hack/setup.sh` | - |
| setup-envtest | `hack/setup.sh`, `.github/workflows/test.yml` | - |
| envtest K8s | `Makefile` | k8s.io/api in go.mod |
| lefthook | `hack/setup.sh` | - |
| gVisor | `hack/setup.sh` | `.github/workflows/e2e.yml` |

## Comment Policy

Only comment non-obvious code. Bad: `// Check if job failed`. Good: explain *why* something unexpected is needed:
```go
// RunAsUser 0 (root) needed to read /proc/<pid>/environ of other users' processes
SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To(int64(0))}
```
