# internal/api-gateway/handlers/

REST API handlers, request/response types, and REST-to-CRD conversion for the api-gateway.

## Files

| File | Purpose |
|------|---------|
| `routes.go` | Huma route registrations (health, sandbox CRUD, filesystem) |
| `types.go` | Request/response types (`CreateSandboxRequest`, `SandboxResponse`, etc.) |
| `convert.go` | REST ↔ CRD type conversion and status mapping |
| `sandbox.go` | Sandbox CRUD handler implementations |
| `filesystem.go` | Filesystem proxy handler (gateway → sidecar) |
| `health.go` | Health and readiness check handlers |

## Key Design Decisions

### REST types are separate from CRD types
Explicit conversion in `convert.go` — no shared structs between REST and K8s layers. This allows the REST API to evolve independently.

### Env vars are write-only
`ContainerSpec` (request) has `Env`; `ContainerInfo` (response) does not. This prevents leaking secrets via the API.

### Status mapping (`conditionsToStatus`)
Maps K8s condition reasons to user-facing status enum: `creating`, `running`, `shuttingDown`, `failed`, `stopped`, `unknown`. The mapping logic lives in `convert.go`.

### Sandbox ID generation
`requestToSandboxCR` generates a 22-char lowercase alphanumeric nanoid (a-z0-9, starts with letter, DNS-1123 safe).

### Resource quantity parsing
Resource strings (e.g. "125m", "512Mi") are parsed via K8s `resource.ParseQuantity`. Invalid values return 400 with the field name.

### Filesystem proxy
`FilesystemHandlers` proxies file upload requests to the sandbox-sidecar via HTTP. Uses `HTTPDoer` interface for testability. Looks up sandbox pod IP from the Sandbox CR status, then forwards the request to `http://<podIP>:<sidecarPort>/filesystem`.

## Testing (`suite_test.go`)

- Ginkgo/Gomega with envtest
- Single cached client from manager (unlike operator's two-client pattern)
- Uses `humatest.TestAPI` for HTTP request/response testing
- Tests use `Eventually()` for cache eventual consistency
- Error injection via controller-runtime `interceptor.Funcs`

## Endpoints

- `POST /sandboxes` → 201, 400, 409
- `GET /sandboxes` → 200
- `GET /sandboxes/{id}` → 200, 404
- `DELETE /sandboxes/{id}` → 204 (idempotent)
- `POST /sandboxes/{id}/filesystem` → 201, 400, 404, 409, 502
- `GET /health`, `/healthz` → 200
- `GET /ready`, `/readyz` → 200, 503
