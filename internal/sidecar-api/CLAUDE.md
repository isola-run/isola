# internal/sidecar-api/

Shared contract types between the api-gateway (client) and sandbox-sidecar (server).

Only types that are identical across both services belong here. Currently contains:

- `FilesystemWriteResponse` — Response type for filesystem write operations (`absolutePath`, `bytesWritten`)

Both the api-gateway and sandbox-sidecar import this package to ensure type safety and prevent drift.
