# CLAUDE.md

## Commands

```bash
# Lint/format (from repo root)
make check-all          # vet + lint + vulncheck (CI-safe)
make fix-all            # Auto-fix formatting and lint issues

# Testing
make test               # Unit tests with coverage
make test-operator FOCUS="TestName"  # Run focused operator test
make test-api           # Run api-gateway tests
make generate           # Regenerate DeepCopy methods after CRD changes
make manifests          # Regenerate CRD YAML after CRD changes

# Build
make build              # Build all binaries to bin/

# Local dev
./hack/setup.sh         # One-time: Kind cluster + registry
tilt up                 # Start dev environment (http://localhost:10350)
# Make sure to keep the Tiltfile updated on changes to the cluster
```

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

After modifying `api/openapi.yaml`:
```bash
make generate-api  # Regenerates internal/api-gateway/generated/openapi.gen.go
```

The generated code is committed to the repo. CI runs `make check-api-codegen` to verify it's in sync.

## Architecture Notes

**Backward compatibility:** not required at this stage. You may introduce breaking changes to CRDs, APIs, and internal interfaces if it simplifies the design, provided tests are updated accordingly.

**Single Go module:** The project uses a single `go.mod` at the root (`github.com/isola-ai/isola-sb`). All binaries import from this module.

**Project structure:**
- `api/v1alpha1/` - CRD type definitions (Sandbox, SandboxTemplate, RootfsSnapshot)
- `api/openapi.yaml` - OpenAPI spec for api-gateway (source of truth for REST API)
- `cmd/operator/` - Kubebuilder operator entry point
- `cmd/api-gateway/` - API gateway for external clients
- `cmd/sandbox-sidecar/` - Sidecar injected into sandbox pods by operator
- `cmd/uploader/` - Snapshot uploader job (uploads tarballs to S3/GCS/Azure)
- `internal/operator/controller/` - Reconciler implementations
- `internal/api-gateway/` - api-gateway handlers, middleware, and generated OpenAPI code
- `internal/sandbox-sidecar/` - Sidecar handlers
- `internal/snapshot/` - Shared snapshot types (used by operator and uploader)
- `charts/` - Helm charts (source of truth for deployment)
- `charts/isola/generated/` - Auto-generated RBAC from kubebuilder annotations (do not edit)

**Default namespaces:** `isola-system` (operator), `isola-sandboxes` (sandbox pods)

**Helm charts are source of truth** - CRDs and RBAC are generated directly to Helm chart via `make manifests`

## CRDs

**Sandbox** - A running sandbox instance. References a SandboxTemplate and optionally embeds NetworkSpec.

**SandboxTemplate** - Reusable pod configuration (image, resources, env vars). Referenced by Sandbox via `templateRef`.

**RootfsSnapshot** - Triggers a snapshot of a sandbox's filesystem. Creates an uploader Job that tarballs the container rootfs and uploads to cloud storage. Supports TTL-based auto-deletion.

## Sharp Edges

**Two-client pattern in tests:** `suite_test.go` uses both `k8sClient` (direct, no cache delay for test writes) and `k8sCache` (cached, required for field index queries). Use the direct client for test assertions.

**Conditions, not phases:** Sandbox status uses K8s conditions (`Ready`, `PodReady`, `NetworkConfigured`), not the deprecated phase pattern.

**Network isolation:**
- Default: deny-all egress with sink DNS (127.0.0.1) so DNS queries fail fast
- Custom rules via `Sandbox.spec.network` (NetworkSpec):
  - `allowAllInternet: true` - allows 0.0.0.0/0 egress (private ranges auto-blocked)
  - `allowedEgressCIDRs` - specific CIDR allowlist
  - `allowedEgressPods` - allow egress to pods matching labels
  - `nameservers` - custom DNS servers (default: sink or 8.8.8.8/1.1.1.1 for internet)
- Network config is **immutable** after sandbox creation
- Static NetworkPolicies deployed via Helm handle base isolation
- Custom per-sandbox NetworkPolicies created by operator when NetworkSpec is set

**Finalizers:** `sandbox.isola.run/cleanup` ensures cleanup before sandbox deletion.

**Field indexing:** Reconcilers index `templateRef` for efficient lookups. See `SetupWithManager()` for index registration.

**Snapshot workflow:**
1. Create RootfsSnapshot CR referencing a sandbox
2. Operator creates a Job with uploader container
3. Uploader tarballs the sandbox container's rootfs via `/proc/<pid>/root`
4. Uploads to cloud storage (S3/GCS/Azure via gocloud.dev)
5. Writes result to termination log, operator reads and updates status
6. TTL controller deletes snapshot after `ttlSecondsAfterCompletion`

## Testing

**Go tests:** Ginkgo/Gomega with envtest (K8s API simulation)

```bash
make test                            # Run all tests
make test-verbose                    # All tests with verbose output
make test-operator                   # Operator tests only
make test-api                        # api-gateway tests only
make test-operator FOCUS="Reconcile" # Focused by Ginkgo pattern
make test GO_TEST_FLAGS="-race"      # With race detector
```

**Variables** (following Cluster API / Kubernetes patterns):
- `FOCUS` - Ginkgo focus pattern for component targets
- `SKIP` - Ginkgo skip pattern
- `GO_TEST_FLAGS` - Additional go test flags

## Tooling Versions

Tool versions are pinned and must be kept in sync:

| Tool | Location | Also sync with |
|------|----------|----------------|
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
