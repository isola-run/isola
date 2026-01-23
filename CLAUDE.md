# CLAUDE.md

## Commands

```bash
# Lint/format (from repo root)
make check-all          # vet + lint + vulncheck (CI-safe)
make fix-all            # Auto-fix formatting and lint issues

# Testing
make test               # Unit tests with coverage
make test-focus FOCUS="TestName"  # Run specific test
make generate           # Regenerate DeepCopy methods after CRD changes
make manifests          # Regenerate CRD YAML after CRD changes

# Build
make build              # Build all binaries to bin/

# Local dev
./hack/setup.sh         # One-time: Kind cluster + registry
tilt up                 # Start dev environment (http://localhost:10350)
cd tests/e2e && uv run pytest  # E2E tests
```

## CRD Workflow

After modifying `api/v1alpha1/*_types.go`:
```bash
make generate manifests && \
  cp config/crd/bases/*.yaml charts/isola-operator/crds/ && \
  cp config/rbac/role.yaml charts/isola-operator/templates/clusterrole.yaml
```

**NetworkTemplateSpec changes:** When modifying `NetworkTemplateSpec`, also update the built-in templates in `charts/isola-operator/templates/network-templates.yaml`. These are Helm-only (not generated) and must match the CRD schema.

## Architecture Notes

**Backward compatibility:** not required at this stage. You may introduce breaking changes to CRDs, APIs, and internal interfaces if it simplifies the design, provided tests are updated accordingly.

**Project structure:**
- `api/v1alpha1/` - CRD type definitions (Sandbox, SandboxTemplate, RootFSSnapshot)
- `cmd/operator/` - Kubebuilder operator entry point
- `cmd/gateway/` - Gin HTTP gateway (auth, K8s client, S3 storage)
- `cmd/agent/` - Gin sidecar injected into sandbox pods by operator
- `cmd/uploader/` - Snapshot uploader job
- `internal/operator/controller/` - Reconciler implementations
- `internal/gateway/` - Gateway handlers and models
- `internal/agent/` - Agent handlers
- `internal/snapshot/` - Shared snapshot types
- `config/` - Kustomize manifests
- `charts/` - Helm charts (source of truth for deployment)
- `tests/e2e/` - Python E2E tests

**Namespaces:** `isola-system` (operator/gateway), `isola-sandboxes` (sandbox pods)

**Helm charts are source of truth** - CRDs must be synced from operator (see workflow above)

## Sharp Edges

**Two-client pattern in tests:** `suite_test.go` uses both `k8sClient` (direct, no cache delay for test writes) and `k8sCache` (cached, required for field index queries). Use the direct client for test assertions.

**Conditions, not phases:** Sandbox status uses K8s conditions (`Ready`, `PodReady`, `NetworkConfigured`), not the deprecated phase pattern.

**Network configuration options:**
- Reference existing NetworkTemplate via `networkTemplateRef`
- Embed spec in `network.spec` (creates owned template, garbage-collected with sandbox)
- Default template: `isola-isolated` (deny all egress, DNS fails fast with sink nameserver)
- Network spec is **immutable** after sandbox creation

**Network isolation architecture:**
- Each NetworkTemplate creates a NetworkPolicy that controls egress
- Ingress from isola-gw is allowed by `allow-isola-gw-ingress` NetworkPolicy (Helm-installed, not per-template)
- Built-in templates in `charts/isola-operator/templates/network-templates.yaml`:
  - `isola-isolated`: Default, denies all traffic. Uses DNSPolicy: None with sink nameserver (127.0.0.1)
  - `isola-egress-only`: Allows internet egress (0.0.0.0/0) with external DNS (8.8.8.8, 1.1.1.1)

**Finalizers:** `sandbox.isola.run/cleanup` ensures cleanup before sandbox deletion.

**Field indexing:** Reconcilers index `templateRef` and `networkTemplateRef` for efficient lookups. See `SetupWithManager()` for index registration.

## Testing

**Go tests:** Ginkgo/Gomega with envtest (K8s API simulation)
```bash
make test
```

**E2E tests:** Python pytest against running cluster
```bash
cd tests/e2e && uv run pytest            # Run all tests
cd tests/e2e && uv run pytest --skip-cleanup  # Debug: keep sandboxes
```

## Comment Policy

Only comment non-obvious code. Bad: `// Check if job failed`. Good: explain *why* something unexpected is needed:
```go
// RunAsUser 0 (root) needed to read /proc/<pid>/environ of other users' processes
SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To(int64(0))}
```
