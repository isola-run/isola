# CLAUDE.md

## Commands

Run `make help` for all available targets. Key commands:

```bash
make check-all                     # All checks, no tests (Go + Python SDK)
make fix-all                       # Auto-fix formatting and lint
make test                          # Unit tests with coverage
make test-operator FOCUS="Name"    # Focused operator test
make test-gateway                  # API gateway tests
make test-e2e                      # E2E tests (requires tilt up)
make openapi                       # Regenerate OpenAPI after handler/route changes
make generate manifests            # Regenerate after CRD type changes
make build                         # Build all binaries

# Local dev
./hack/setup.sh                    # One-time: Kind cluster + registry
tilt up                            # Start dev environment (http://localhost:10350)
```

Keep the Tiltfile updated when changing cluster resources.

## Critical Rules

**NEVER GUESS technical details** -- version numbers, import paths, API endpoints, CLI flags, config formats. Always search/verify first, ask the user if unsure, or test it.

**Backward compatibility** is not required. Breaking changes to CRDs, APIs, and internal interfaces are fine if they simplify the design, provided tests are updated.

## Generated Files (do not edit manually)

After modifying `api/v1alpha1/*_types.go`, run `make generate manifests`:
- CRDs -> `charts/isola/crds/`
- RBAC -> `charts/isola/generated/role.yaml`

After modifying handler input/output types or route registrations, run `make openapi`:
- `api/openapi/api-gateway.yaml` and `api/openapi/sandbox-sidecar.yaml`
- CI runs `make check-openapi` to verify specs are in sync

## Cross-Cutting Constraints

**Gateway is a thin passthrough** -- it validates input structure (via Huma struct tags) but does not apply domain defaults or domain validation. That is the operator's job. Do not add default logic or sandbox-existence checks to the gateway.

**REST/CRD validation alignment:** Huma struct tags on REST types (e.g., `maxItems:"16"`, `pattern:"..."`) must match the kubebuilder markers on the corresponding CRD type fields. Adding a validation to one layer without the other causes confusing errors.

**Identifier vocabulary:** REST API uses `id` for a resource's own system-generated identifier and `{resource}Id` for cross-resource references (e.g., `sandboxId` in rootfs snapshot requests/responses). User-chosen identifiers use `name` (e.g., `snapshotName`). The conversion layer maps REST vocabulary to CRD vocabulary (e.g., `sandboxId` → CRD `sandboxName`).

**Env vars are write-only:** Request types accept env vars; response types intentionally omit them to avoid leaking secrets. Do not add `Env` to response types.

**Compile-time type assertions** in gateway command/filesystem handlers (`_ = sidecarapi.CreateCommandRequest(CreateCommandRequest{})`) enforce structural compatibility with sidecar-api contract types. Do not remove these.

**Long-poll timeout chain** -- values MUST satisfy: SDK (20s) < gateway max (25s) < sidecar max (30s) < gateway WriteTimeout (45s) < sidecar WriteTimeout (75s). Changing any value without adjusting the others causes cascading failures. Locations: SDK `_commands.py`, gateway `command.go`, sidecar `command.go`, gateway `main.go`, sidecar `main.go`.

**Operator metrics** use `promauto.With(metrics.Registry)` in `internal/operator/controller/metrics.go`. New metrics must use this registry, not the default.

**Rootfs restore retry requires K8s 1.34+:** Uses `ContainerRestartRules` feature gate (alpha in 1.34, beta/on-by-default in 1.35+). Without it, `restartPolicyRules` fields are silently ignored and containers fail permanently on missing tar.

## Testing

**Two-client pattern in operator tests:** `suite_test.go` uses `k8sClient` (direct, no cache delay) for test writes/assertions and `k8sCache` (cached, for field index queries). Use the direct client for new test assertions.

**Python SDK:** `sdks/python/`, managed with uv. `Network` has manual `Field(alias=...)` overrides for acronyms (`allowClusterDNS`, `allowedEgressCIDRs`) that `to_camel` cannot handle -- new fields with acronyms need the same treatment. For SDK polling on eventually consistent resources, finite deadlines must still be enforced on `NotFoundError` branches; otherwise cache lag can turn bounded waits into unbounded ones.

## Tooling Versions

Tool versions are pinned and must be kept in sync across locations:

| Tool | Location | Also sync with |
|------|----------|----------------|
| Go | `Makefile` (`GO_VERSION`), `go.mod` | Dockerfile `FROM golang:` tags |
| golangci-lint | `hack/setup.sh` | `.github/workflows/lint.yml` |
| govulncheck | `hack/setup.sh` | - |
| lefthook | `hack/setup.sh` | - |
| setup-envtest | `hack/setup.sh` | `.github/workflows/test.yml` |
| envtest K8s | `Makefile` | k8s.io/api in go.mod |
| controller-gen | `hack/setup.sh` | `.github/workflows/codegen.yml` |
| gVisor | `hack/setup.sh` | `.github/workflows/e2e.yml` |
| Python | `sdks/python/pyproject.toml` | CI workflows |

## Comment Policy

Only comment non-obvious code. Explain *why*, not *what*:
```go
// RunAsUser 0 (root) needed to read /proc/<pid>/environ of other users' processes
SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To(int64(0))}
```
