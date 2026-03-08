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
make generate           # Regenerate DeepCopy methods after CRD changes
make manifests          # Regenerate CRD YAML after CRD changes

# Testing (Python SDK)
make test-sdk-python    # Run Python SDK tests
make test-sdk-python-verbose  # Run with verbose output

# Testing (E2E) — requires running cluster: tilt up
make test-e2e           # Run E2E tests
make test-e2e-verbose   # Verbose output
make test-e2e FOCUS="pattern"  # Run specific tests matching pattern

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

## Generated Files (do not edit manually)

After modifying `api/v1alpha1/*_types.go`:
```bash
make generate manifests
```
- CRDs → `charts/isola/crds/`
- RBAC → `charts/isola/generated/role.yaml`

The Helm `clusterrole.yaml` template uses `.Files.Get` to include the generated RBAC rules
with proper Helm templating for name/labels.

After modifying handler input/output types or route registrations:
```bash
make openapi
```
- `api/openapi/api-gateway.yaml` - End-user facing API
- `api/openapi/sandbox-sidecar.yaml` - Internal API (api-gateway → sidecar)

CI runs `make check-openapi` to verify generated specs are in sync.

## Architecture Notes

**Backward compatibility:** not required at this stage. You may introduce breaking changes to CRDs, APIs, and internal interfaces if it simplifies the design, provided tests are updated accordingly.

**Single Go module:** The project uses a single `go.mod` at the root (`github.com/isola-ai/isola`). All binaries import from this module.

**Multi-service architecture:** Four binaries in `cmd/` — `operator`, `api-gateway`, `sandbox-sidecar`, `uploader`. Each has its own `internal/` packages under the matching path. The api-gateway uses domain sub-packages (`{health,sandbox,filesystem,command}/`) with shared utilities in `proxy.go`. The sandbox-sidecar uses domain sub-packages (`{health,filesystem,command}/`) with shared utilities in `sidecar.go`. Cross-service packages:
- `internal/sidecar-api/` - Shared contract types between api-gateway and sandbox-sidecar
- `internal/snapshot/` - Shared types used by both operator and uploader
- `internal/sandbox-sidecar/proc/` - Procfs abstraction for container PID discovery and filesystem access via `/proc/<pid>/root`
- `internal/httputil/` - Deadline/timeout protection for streaming I/O (per-operation write/read deadlines via `http.ResponseController`)
- `internal/sseutil/` - SSE (Server-Sent Events) writer for streaming command stdout/stderr with UTF-8 encoding, keepalive, and offset-based resume via `id:` fields

**Default namespaces:** `isola-system` (operator), `isola-sandboxes` (sandbox pods)

**Operator metrics:** Custom Prometheus metrics are defined in `internal/operator/controller/metrics.go` and auto-registered via `promauto.With(metrics.Registry)`. Helm includes a metrics Service and optional ServiceMonitor (`serviceMonitor.enabled: false` by default). Tiltfile port-forwards operator metrics to `localhost:8082`.

## Python SDK (`sdks/python/`)

**Package:** `isola` — thin client for the api-gateway REST API. Uses httpx for HTTP and pydantic for models. Managed with uv.

**Client initialization:** `Isola(base_url=...)` or via `ISOLA_BASE_URL` env var. Built-in retry logic (max 5 retries with 1s backoff) for transient errors (connection failures, 502/503/504).

**Dual sync/async pattern:** Every public class has a sync and async variant — `Isola`/`AsyncIsola`, `Sandbox`/`AsyncSandbox`, `Command`/`AsyncCommand`, `Commands`/`AsyncCommands`, `Filesystem`/`AsyncFilesystem`. Internal API clients follow the same split: `_SyncAPI`/`_AsyncAPI`.

**Object hierarchy:** `Isola` → `client.sandboxes.create()` returns a `Sandbox` → `sandbox.commands` (Commands), `sandbox.filesystem` (Filesystem). Each resource object holds a reference to the underlying API client and the sandbox ID.

**Command execution (`_commands.py`):** Two modes — `spawn(*args)` returns a `Command` immediately (non-blocking), `run(*args)` waits and returns a `CommandResult` (with `stdout`, `stderr`, `exit_code`). `Command` provides `.stdout`/`.stderr` (`StreamReader`), `.wait()` (long-polls), `.exit_code()`, `.write_stdin()`, `.close_stdin()`, `.kill()`. Long-poll interval: 20s (must stay <= gateway max of 25s).

**Pydantic models (`_models.py`):** All models extend `IsolaModel` which uses `to_camel` alias generator for Python snake_case ↔ API camelCase. Models accept both forms (`validate_by_name=True, validate_by_alias=True`). `extra="ignore"` so the server can add fields without breaking the client. `NetworkSpec` has manual `Field(alias=...)` overrides for acronyms (`allowClusterDNS`, `allowedEgressCIDRs`) that `to_camel` can't handle.

**Streaming (`_streaming.py`):** `StreamReader`/`AsyncStreamReader` consume SSE streams (via `httpx-sse`) with auto-reconnect (exponential backoff, offset-based resume via `Last-Event-ID` header, max 5 reconnects). Iterable (`for chunk in cmd.stdout`) or bulk read (`cmd.stdout.read()`).

**Error hierarchy:** `IsolaError` base → `APIError` → status-specific subclasses (`BadRequestError`, `NotFoundError`, `ConflictError`, `ValidationError`, `InternalError`, `BadGatewayError`) mapped from HTTP status codes. `APIConnectionError` for transport failures. `is_transient()` helper identifies retryable errors.

**Testing:** pytest + pytest-asyncio (auto mode) + respx for HTTP mocking. Tests use fake API/response objects rather than respx routes for streaming tests. Strict mypy type checking enabled.

**Linting:** ruff (format + lint), mypy (strict). Run `make sdk-python-check-all` for lint + typecheck, `make sdk-python-fix-all` for auto-fix.

## CRDs

**Sandbox** - A running sandbox instance. Key spec fields:
- `podTemplate` (required) - Inlined pod template with containers, volumes, etc.
- `activeDeadlineSeconds` - Max lifetime; operator calculates `status.timeoutAt` from pod start time
- `shutdownPolicy` - ShutdownPolicy struct with `strategy` (`Delete` default, or `SnapshotRootfs`) and optional `activeDeadlineSeconds` for the snapshot job
- `network` - NetworkSpec for isolation rules (immutable after creation)

**RootfsSnapshot** - Triggers a snapshot of a sandbox's filesystem. Creates an uploader Job that tarballs the container rootfs and uploads to cloud storage. Key spec fields:
- `sandboxName` (required) - The sandbox to snapshot
- `snapshotName` (optional) - Name used for the storage key (`rootfssnapshots/<snapshotName>.tar`); defaults to sandbox name
- `container` (optional) - Which container to snapshot; defaults to first
- Supports TTL-based auto-deletion via `ttlSecondsAfterFinished`

## REST API (api-gateway)

The api-gateway is a thin passthrough to K8s — it validates input structure but does not apply domain defaults (that's the operator's job). Uses Huma framework on chi router.

**Endpoints:**
- `POST /sandboxes` — create
- `GET /sandboxes` — list (not paginated)
- `GET /sandboxes/{id}` — get details
- `DELETE /sandboxes/{id}` — delete (idempotent)
- `POST /sandboxes/{id}/filesystem` — file upload (proxied to sidecar)
- `GET /sandboxes/{id}/filesystem` — file download (proxied to sidecar)
- `POST /sandboxes/{id}/commands` — start command (proxied to sidecar, 202 Accepted). Request body uses `args` (not `cmd`): `args[0]` is executable, `args[1:]` are arguments. Optional `timeout` (seconds), `env`, `cwd`.
- `GET /sandboxes/{id}/commands/{cmdId}/status` — exit code (null if running). Supports long-polling via `?waitSeconds=N` (max 25 at gateway, max 30 at sidecar).
- `GET /sandboxes/{id}/commands/{cmdId}/stdout` — SSE stream of stdout (`text/event-stream`). Resume via `Last-Event-ID` header (byte offset).
- `GET /sandboxes/{id}/commands/{cmdId}/stderr` — SSE stream of stderr (`text/event-stream`). Resume via `Last-Event-ID` header (byte offset).
- `POST /sandboxes/{id}/commands/{cmdId}/stdin` — write to stdin
- `POST /sandboxes/{id}/commands/{cmdId}/stdin/close` — close stdin pipe
- `DELETE /sandboxes/{id}/commands/{cmdId}` — kill command (idempotent)

**REST ↔ CRD conversion (`sandbox/convert.go`):**
REST types are separate from CRD types with explicit conversion in `sandbox/convert.go`. Key behaviors:
- User-facing status enum: `creating`, `running`, `shuttingDown`, `failed`, `stopped`, `unknown`

**Env vars are write-only:** Request types accept env vars but response types intentionally omit them to avoid leaking secrets. `ContainerSpec` (request) has `Env`; `ContainerInfo` (response) does not.

**Default command:** Containers with no `command` get `["sleep", "infinity"]` injected by the operator so sandboxes stay alive by default. The gateway passes `command` through as-is (nil when omitted); the operator applies the default at pod creation time.

## Sharp Edges

**Two-client pattern in operator tests:** `suite_test.go` uses both `k8sClient` (direct, no cache delay for test writes) and `k8sCache` (cached, required for field index queries). Use the direct client for test assertions. The api-gateway tests use a single cached client from the manager.

**Conditions, not phases:** Sandbox status uses K8s conditions (`Ready`, `PodReady`, `NetworkConfigured`), not the deprecated phase pattern.

**Network isolation:**
- Default: deny-all egress with sink DNS (127.0.0.1) so DNS queries fail fast
- Custom rules via `Sandbox.spec.network` (NetworkSpec):
  - `allowInternetEgress: true` - allows 0.0.0.0/0 egress (private ranges auto-blocked)
  - `allowedEgressCIDRs` - specific CIDR allowlist
  - `nameservers` - custom DNS servers (no automatic default — user must specify explicitly; without them, sink DNS 127.0.0.1 is used)
- Static NetworkPolicies deployed via Helm handle base isolation
- Custom per-sandbox NetworkPolicy created by operator only when CIDRs or custom nameservers are specified (and `allowInternetEgress` is not true)

**Finalizers:** `sandbox.isola.run/cleanup` ensures cleanup before sandbox deletion.

**Snapshot workflow:**
1. Create RootfsSnapshot CR referencing a sandbox
2. Operator creates a Job with uploader container
3. Uploader tarballs the sandbox container's rootfs via `/proc/<pid>/root`
4. Uploads to cloud storage (S3/GCS/Azure via gocloud.dev)
5. Writes result to termination log, operator reads and updates status
6. TTL controller deletes snapshot after `ttlSecondsAfterFinished`

**Command execution:**
- Commands run via `SysProcAttr.Chroot` to `/proc/<pid>/root`, entering the sandbox container's filesystem view without changing namespaces. The sidecar uses `/bin/sh -c 'exec "$@"'` for PATH lookup inside the container, and `exec` replaces the shell so SIGKILL reaches the user's process directly. Requires `CAP_SYS_PTRACE` (for gVisor's `/proc/<pid>/root` access check) and `CAP_SYS_CHROOT` (default capability set).
- Non-blocking model: POST returns 202 immediately with a `commandId`; status is polled via long-polling (`?waitSeconds=N`).
- Per-command `timeout` (seconds): enforced via `context.WithTimeout` → SIGKILL on expiration (exit code -1).
- Command stdout/stderr are stored on the container's ephemeral storage (counts against resource limits).
- Command state is in-memory in the sidecar — all running/completed commands are lost on sidecar restart.
- Stdout/stderr are streamed as SSE (`text/event-stream`). The sidecar produces SSE via `internal/sseutil/` (UTF-8 sanitization, `id:` fields for byte-offset resume, 15s keepalive comments). The gateway proxies SSE transparently. The Python SDK consumes SSE via `httpx-sse` and resumes with `Last-Event-ID` header. Non-UTF-8 bytes in output are replaced with U+FFFD; for binary content, redirect to a file and download via the filesystem API.
- Streaming I/O uses per-operation deadline protection (10s via `internal/httputil`) to detect stalled connections.
- Gateway command handlers use compile-time type assertions (`_ = sidecarapi.CreateCommandRequest(CreateCommandRequest{})`) to enforce structural compatibility with `sidecar-api` contract types. Do not remove these.

**Long-poll timeout chain:** Values must satisfy: SDK (20s) < gateway max (25s) < sidecar max (30s) < gateway WriteTimeout (45s) < sidecar WriteTimeout (75s). Changing any one value without adjusting the others can cause cascading failures (premature disconnects or stalled connections). Locations: SDK `_commands.py` (`_LONG_POLL_WAIT_SECONDS`), gateway `command.go` (`maximum:"25"`), sidecar `command.go` (`maximum:"30"`), gateway `main.go` (`serverWriteTimeout`), sidecar `main.go` (`serverWriteTimeout`).

**Dockerfiles copy all of internal/:** Each binary's Dockerfile copies the entire `internal/` directory rather than individual packages. This avoids needing to update Dockerfiles and Tiltfile when adding new `internal/` packages.

## Testing

**Go tests:** Ginkgo/Gomega with envtest (K8s API simulation)

**Variables** (following Cluster API / Kubernetes patterns):
- `FOCUS` - Ginkgo focus pattern for component targets
- `SKIP` - Ginkgo skip pattern
- `GO_TEST_FLAGS` - Additional go test flags

**Operator tests** use a `FakeClock` (internal `Clock` interface) for deterministic timeout and snapshot testing — no flaky time.Sleep waits. Test fixtures in `internal/testutil/utils/fixtures.go` use functional options (`WithSandboxActiveDeadline`, `WithNetworkSpec`, `WithInternetAccess`, etc.).

**API gateway tests** use `humatest.TestAPI` for HTTP request/response testing against a real envtest K8s backend. Tests use `Eventually()` for cache eventual consistency. Error injection tests use controller-runtime's `interceptor.Funcs` to inject fake K8s API errors.

**Python SDK tests** use pytest + pytest-asyncio with `asyncio_mode = "auto"`. HTTP mocking via respx for client/sandbox/filesystem tests. Streaming tests use hand-rolled fake API/response objects (not respx) to simulate reconnects, network errors, and chunked delivery. Run with `make test-sdk-python`.

**E2E tests** (`tests/e2e/`) use pytest + pytest-asyncio against a live cluster (requires `tilt up`). Both sync and async fixtures. Base URL from `ISOLA_BASE_URL` env var (defaults to `http://localhost:8080`). Metrics URL from `ISOLA_METRICS_URL` (defaults to `http://localhost:8082`). Covers commands, streaming, filesystem, network isolation, timeouts, error handling, lifecycle, and operator metrics. Run with `make test-e2e`.

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
| controller-gen | `hack/setup.sh` | `.github/workflows/codegen.yml` |
| gVisor | `hack/setup.sh` | `.github/workflows/e2e.yml` |
| Python | `.python-version` | `sdks/python/pyproject.toml` `requires-python` |
| uv | - | Manages `sdks/python/uv.lock` |
| ruff | `sdks/python/pyproject.toml` | - |
| mypy | `sdks/python/pyproject.toml` | - |

## Comment Policy

Only comment non-obvious code. Bad: `// Check if job failed`. Good: explain *why* something unexpected is needed:
```go
// RunAsUser 0 (root) needed to read /proc/<pid>/environ of other users' processes
SecurityContext: &corev1.SecurityContext{RunAsUser: ptr.To(int64(0))}
```
