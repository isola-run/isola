# internal/sandbox-sidecar/proc/

`/proc` filesystem abstraction for discovering container processes within sandbox pods.

## Interface

`ProcFS` interface with implementations:
- `RealProcFS` — Reads the actual `/proc` filesystem (production)
- Test doubles can be created by implementing the interface

## Key Methods

- `FindMarkedPID(containerName)` — Scans `/proc/*/environ` for processes with `ISOLA_CONTAINER_NAME=<name>`. If containerName is empty and only one container exists, returns that container's PID.
- `GetCwd(pid)` — Reads `/proc/<pid>/cwd` symlink
- `GetRoot(pid)` — Returns `/proc/<pid>/root` path
- `GetUIDGID(pid)` — Reads real UID/GID from `/proc/<pid>/status`

## How Container Discovery Works

The operator injects `ISOLA_CONTAINER_NAME` as an environment variable into each sandbox container. The sidecar scans `/proc/<pid>/environ` for all PIDs to find processes with this marker. This allows the sidecar to discover the container's PID namespace entry without relying on container runtime APIs.

## Error

`ErrContainerNotFound` is returned when no process has the `ISOLA_CONTAINER_NAME` marker.
