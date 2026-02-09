# cmd/sandbox-sidecar/

Sidecar container entry point. Injected into sandbox pods by the operator to provide filesystem access to the sandbox container.

## Configuration

- `--log-level` / `ISOLA_LOG_LEVEL` — Log level (default: `info`)
- `--dev` / `ISOLA_DEV_MODE` — Enable development mode (text logging)

## Architecture

- Uses **Huma** framework on **chi** router
- Listens on port `10032` (defined in `internal/constants`)
- Registers health and filesystem route handlers
- Uses `RealProcFS` to discover container PIDs via `/proc/<pid>/environ`

## Key Behavior

- No graceful shutdown currently implemented
- Filesystem operations use `/proc/<pid>/root` to access the sandbox container's filesystem
- PID discovery works by scanning `/proc` for processes with `ISOLA_CONTAINER_NAME` env var
