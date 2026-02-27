---
sidebar_position: 3
title: Commands
slug: /api/commands
---

# Commands API

## Start a Command

```
POST /sandboxes/{id}/commands
```

Starts a new command in the sandbox container. Returns a command ID for tracking. Commands always run as root (UID 0, GID 0).

### Parameters

| Parameter | Location | Type | Required | Description |
|-----------|----------|------|----------|-------------|
| `id` | path | string | Yes | Sandbox identifier |
| `container` | query | string | No | Container name. Defaults to the only container. |

### Request Body

```json
{
  "cmd": "/bin/bash",
  "args": ["-c", "echo hello"],
  "env": {
    "MY_VAR": "override"
  },
  "cwd": "/app",
  "timeout": 30
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cmd` | string | Yes | Executable path |
| `args` | string[] | No | Arguments to the executable |
| `env` | map | No | Environment variable overrides |
| `cwd` | string | No | Working directory (defaults to container's cwd) |
| `timeout` | int | No | Max execution time in seconds |

### Response

**`202 Accepted`**

```json
{
  "commandId": "550e8400-e29b-41d4-a716-446655440000"
}
```

---

## Get Command Status

```
GET /sandboxes/{id}/commands/{cmdId}/status
```

Returns the exit code of the command, or `null` if still running.

### Parameters

| Parameter | Location | Type | Description |
|-----------|----------|------|-------------|
| `id` | path | string | Sandbox identifier |
| `cmdId` | path | string | Command identifier (UUID) |

### Response

**`200 OK`**

```json
{
  "exitCode": 0
}
```

When the command is still running:

```json
{
  "exitCode": null
}
```

---

## Stream stdout

```
GET /sandboxes/{id}/commands/{cmdId}/stdout
```

Streams the command's stdout as raw bytes. The connection remains open until the command exits.

### Parameters

| Parameter | Location | Type | Required | Description |
|-----------|----------|------|----------|-------------|
| `id` | path | string | Yes | Sandbox identifier |
| `cmdId` | path | string | Yes | Command identifier (UUID) |
| `offset` | query | int | No | Byte offset to resume from (default: 0) |

### Response

**`200 OK`**

Content-Type: `application/octet-stream`

The response is a streaming byte stream. Use the `offset` parameter to resume from a specific position after a disconnection.

---

## Stream stderr

```
GET /sandboxes/{id}/commands/{cmdId}/stderr
```

Identical to stdout streaming but for the command's stderr.

### Parameters

| Parameter | Location | Type | Required | Description |
|-----------|----------|------|----------|-------------|
| `id` | path | string | Yes | Sandbox identifier |
| `cmdId` | path | string | Yes | Command identifier (UUID) |
| `offset` | query | int | No | Byte offset to resume from (default: 0) |

### Response

**`200 OK`**

Content-Type: `application/octet-stream`

---

## Write to stdin

```
POST /sandboxes/{id}/commands/{cmdId}/stdin
```

Writes raw bytes to the command's stdin.

### Parameters

| Parameter | Location | Type | Description |
|-----------|----------|------|-------------|
| `id` | path | string | Sandbox identifier |
| `cmdId` | path | string | Command identifier (UUID) |

### Request Body

Content-Type: `application/octet-stream`

Raw bytes to write to stdin.

### Response

**`204 No Content`**

---

## Kill a Command

```
DELETE /sandboxes/{id}/commands/{cmdId}
```

Kills the command process. This operation is **idempotent** — killing an already-exited command is a no-op.

### Parameters

| Parameter | Location | Type | Description |
|-----------|----------|------|-------------|
| `id` | path | string | Sandbox identifier |
| `cmdId` | path | string | Command identifier (UUID) |

### Response

**`204 No Content`**
