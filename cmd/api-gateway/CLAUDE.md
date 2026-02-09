# cmd/api-gateway/

REST API gateway entry point. Serves the external-facing sandbox management API.

## Configuration

- `--http-port` / `ISOLA_HTTP_PORT` — HTTP server port (default: 8080)
- `--sandbox-namespace` / `ISOLA_SANDBOX_NAMESPACE` (required) — Namespace where sandboxes are created
- `--log-level` / `ISOLA_LOG_LEVEL` — Log level (default: `info`)
- `--dev` / `ISOLA_DEV_MODE` — Enable development mode (text logging)

## Architecture

- Uses **Huma** framework on **chi** router for REST API
- Uses **controller-runtime** manager for K8s client with cache (scoped to sandbox namespace)
- Registers three handler groups: health, sandbox CRUD, filesystem operations
- Filesystem operations proxy to sandbox-sidecar via HTTP
- Graceful shutdown with 30-second grace period

## Key Behavior

- The gateway is a thin passthrough to K8s — validates input structure but does not apply domain defaults (operator's job)
- Creates an `*http.Client` for sidecar communication with redirect following disabled
- Cache is namespace-scoped to `sandboxNamespace` and watches only Sandbox and RootfsSnapshot objects
