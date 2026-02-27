---
sidebar_position: 3
title: Commands
---

# Commands

Isola allows you to execute commands inside sandbox containers through the API gateway and sidecar.

## How Commands Work

1. You send a command request to the API gateway
2. The gateway proxies the request to the sandbox sidecar (running on port `10032` inside the pod)
3. The sidecar uses `nsenter` to enter the sandbox container's namespaces
4. The command runs with the container's environment and filesystem
5. Stdout and stderr are captured and stored on the container's ephemeral storage
6. Output can be streamed back to the client

### nsenter Execution

Commands are executed using `nsenter --all --target <pid> --no-fork`, which enters all of the sandbox container's namespaces (mount, UTS, IPC, net, PID, etc.). The `--no-fork` flag is critical — it prevents forking when entering the PID namespace, ensuring signals like SIGKILL reach the actual process.

The sidecar discovers the sandbox container's PID by scanning `/proc` for processes with the `ISOLA_CONTAINER_NAME` environment variable, which is injected by the operator.

## Creating Commands

```json
POST /sandboxes/{id}/commands

{
  "cmd": "/bin/bash",
  "args": ["-c", "echo hello world"],
  "env": {"MY_VAR": "override"},
  "cwd": "/app",
  "timeout": 30
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cmd` | string | Yes | Executable path |
| `args` | string[] | No | Arguments to the executable |
| `env` | map | No | Environment variable overrides |
| `cwd` | string | No | Working directory (defaults to the container's cwd) |
| `timeout` | int | No | Max execution time in seconds |

The response is `202 Accepted` with a `commandId` for tracking:

```json
{
  "commandId": "550e8400-e29b-41d4-a716-446655440000"
}
```

## Streaming Output

Command stdout and stderr are streamed as raw bytes:

```
GET /sandboxes/{id}/commands/{cmdId}/stdout
GET /sandboxes/{id}/commands/{cmdId}/stderr
```

The connection stays open until the command exits. Supports resuming via the `?offset=N` query parameter, where `N` is the byte offset to resume from.

### Auto-Reconnect

The Python SDK handles reconnection automatically with exponential backoff:
- Initial backoff: 0.1s
- Backoff factor: 2x
- Max backoff: 5s
- Max reconnects: 5

## Checking Status

```
GET /sandboxes/{id}/commands/{cmdId}/status
```

Returns:
```json
{
  "exitCode": 0
}
```

The `exitCode` is `null` if the command is still running.

## Writing to stdin

```
POST /sandboxes/{id}/commands/{cmdId}/stdin
Content-Type: application/octet-stream

<raw bytes>
```

## Killing Commands

```
DELETE /sandboxes/{id}/commands/{cmdId}
```

This is idempotent — killing an already-exited command is a no-op.

## Important Notes

- Commands always run as root (UID 0, GID 0)
- Command state is **in-memory** in the sidecar — all running and completed commands are lost if the sidecar restarts
- Stdout/stderr data is stored on the container's ephemeral storage and counts against resource limits
- The `?container` query parameter can specify which container to run in (defaults to the only container)
