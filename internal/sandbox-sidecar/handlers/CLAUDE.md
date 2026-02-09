# internal/sandbox-sidecar/handlers/

HTTP handlers for the sandbox-sidecar service.

## Files

| File | Purpose |
|------|---------|
| `routes.go` | Huma route registrations (health, filesystem) |
| `filesystem.go` | File write handler — resolves path, writes via `/proc/<pid>/root` |
| `health.go` | Simple health check handler |

## Endpoints

- `GET /health`, `/healthz` → 200
- `POST /filesystem` → 201, 400, 500

## Filesystem Handler Design

- Uses `proc.ProcFS` interface to find the sandbox container's PID
- **PID caching**: Caches discovered PIDs in a `sync.RWMutex`-protected map, validates cache entries by re-checking `/proc/<pid>/environ`
- Path resolution: relative paths are resolved against the container's cwd (`/proc/<pid>/cwd`)
- File writes go through `/proc/<pid>/root/<path>` to access the container's filesystem
- Files are written with the container process's UID/GID (read from `/proc/<pid>/status`)
