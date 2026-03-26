# CLAUDE.md

## Commands

```bash
# Lint/format (from repo root)
make check-all          # All checks, no tests (Go + Python SDK, CI-safe)
make fix-all            # Auto-fix formatting and lint issues (Go + Python SDK)

# Testing (Go)
make test               # Unit tests with coverage
make test-verbose       # All tests with verbose output
make test-operator FOCUS="TestName"  # Run focused operator test
make test-gateway       # Run api-gateway tests
make test-sidecar       # Run sandbox-sidecar tests
make test GO_TEST_FLAGS="-race"  # With race detector

# Testing (Python SDK — sdks/python/)
make test-sdk-python    # Run Python SDK tests

# Testing (E2E) — requires running cluster: tilt up
make test-e2e           # Run E2E tests in parallel (20 workers, skips @slow)
make test-e2e FOCUS="pattern"  # Run specific tests matching pattern

# Build & codegen
make build              # Build all binaries to bin/
make generate manifests # Regenerate after CRD type changes
make openapi            # Regenerate after handler/route changes

# Local dev
./hack/setup.sh         # One-time: Kind cluster + registry
tilt up                 # Start dev environment (http://localhost:10350)
```

## Critical Rules

**NEVER GUESS technical details** — version numbers, import paths, API endpoints, CLI flags, config formats. Always search/verify first, ask the user if unsure, or test it.

**Backward compatibility** is not required. Breaking changes to CRDs, APIs, and internal interfaces are fine if they simplify the design, provided tests are updated.

## Generated Files (do not edit manually)

After modifying `api/v1alpha1/*_types.go`:
```bash
make generate manifests
```
- CRDs → `charts/isola/crds/`
- RBAC → `charts/isola/generated/role.yaml` (included via `.Files.Get` in Helm `clusterrole.yaml`)

After modifying handler input/output types or route registrations:
```bash
make openapi
```
- `api/openapi/api-gateway.yaml` and `api/openapi/sandbox-sidecar.yaml`
- CI runs `make check-openapi` to verify specs are in sync

## Architecture

**Single Go module** at root (`github.com/isola-ai/isola`). Six binaries in `cmd/` — `operator`, `api-gateway`, `sandbox-sidecar`, `uploader`, `snapshot-mounter`, `openapi-gen`.

**Default namespaces:** `isola-system` (operator), `isola-sandboxes` (sandbox pods).

**Python SDK** (`sdks/python/`): `isola` package, managed with uv. Uses httpx + pydantic. Dual sync/async public API. Lint: `make sdk-python-check-all` / `make sdk-python-fix-all`.

**REST API** (api-gateway): thin passthrough to K8s using Huma on chi router. All endpoints under `/v1/`. See `api/openapi/api-gateway.yaml` for the full endpoint list.

## Sharp Edges

**Env vars are write-only:** Request types accept env vars; response types omit them to avoid leaking secrets.

**Default command:** Containers with no `command` get `["sleep", "infinity"]` injected by the operator. The gateway passes `command` through as-is (nil when omitted).

**Command args, not cmd:** The command endpoint uses `args` (not `cmd`): `args[0]` is the executable, `args[1:]` are arguments.

**Compile-time type assertions** in gateway command handlers (`_ = sidecarapi.CreateCommandRequest(CreateCommandRequest{})`) enforce structural compatibility with sidecar-api contract types. Do not remove these.

**Long-poll timeout chain** — values MUST satisfy: SDK (20s) < gateway max (25s) < sidecar max (30s) < gateway WriteTimeout (45s) < sidecar WriteTimeout (75s). Locations: SDK `_commands.py` (`_LONG_POLL_WAIT_SECONDS`), gateway `command.go` (`maximum:"25"`), sidecar `command.go` (`maximum:"30"`), gateway `main.go` (`serverWriteTimeout`), sidecar `main.go` (`serverWriteTimeout`).

**Conditions, not phases:** Sandbox status uses K8s conditions (`Ready`, `PodReady`, `NetworkConfigured`), not the deprecated phase pattern.

**Network isolation defaults:** deny-all egress with sink DNS (127.0.0.1). Custom per-sandbox NetworkPolicy is created by operator only when CIDRs or custom nameservers are specified (and `allowInternetEgress` is not true).

**Rootfs restore retry requires K8s 1.34+:** Uses `ContainerRestartRules` feature gate (alpha in 1.34, beta in 1.35+). Without it, `restartPolicyRules` fields are silently ignored and containers fail permanently on missing tar.

**Dockerfiles copy all of `internal/`** to avoid updating Dockerfiles/Tiltfile when adding new packages.

## Testing Patterns

**Go tests:** Ginkgo/Gomega with envtest. Variables: `FOCUS` (focus pattern), `SKIP` (skip pattern), `GO_TEST_FLAGS`.

**Two-client pattern in operator tests:** `suite_test.go` uses `k8sClient` (direct, for test writes/assertions) and `k8sCache` (cached, for field index queries). The api-gateway tests use a single cached client from the manager.

**Operator tests** use `FakeClock` for deterministic timeout/snapshot testing — no `time.Sleep`. Test helpers use inline functional options for sandbox customization.

**API gateway tests** use `humatest.TestAPI` against envtest. Use `Eventually()` for cache consistency. Error injection via `interceptor.Funcs`.

**E2E tests** (`tests/e2e/`): pytest + pytest-asyncio against a live cluster. `ISOLA_BASE_URL` (default `http://localhost:8080`), `ISOLA_METRICS_URL` (default `http://localhost:8082`).

## Tooling Versions

Tool versions are pinned and must be kept in sync:

| Tool | Location | Also sync with |
|------|----------|----------------|
| Go | `Makefile` (`GO_VERSION`), `go.mod` | Dockerfile `FROM golang:` tags |
| golangci-lint | `hack/setup.sh` | `.github/workflows/lint.yml` |
| setup-envtest | `hack/setup.sh` | `.github/workflows/test.yml` |
| envtest K8s | `Makefile` | k8s.io/api in go.mod |
| controller-gen | `hack/setup.sh` | `.github/workflows/codegen.yml` |
| gVisor | `hack/setup.sh` | `.github/workflows/e2e.yml` |
| Python | `sdks/python/pyproject.toml` `requires-python` | CI workflows |

## Comment Policy

Only comment non-obvious code. Bad: `// Check if job failed`. Good: explain *why* something unexpected is needed:
```go
// RunAsUser 0 (root) needed to read /proc/<pid>/environ of other users' processes
SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To(int64(0))}
```
