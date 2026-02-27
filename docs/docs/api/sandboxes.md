---
sidebar_position: 2
title: Sandboxes
slug: /api/sandboxes
---

# Sandboxes API

## Create a Sandbox

```
POST /sandboxes
```

### Request Body

```json
{
  "podTemplate": {
    "container": {
      "image": "python:3.12",
      "command": ["/bin/bash"],
      "env": {
        "MY_VAR": "value"
      },
      "resources": {
        "requests": {
          "cpu": "100m",
          "memory": "256Mi"
        },
        "limits": {
          "cpu": "500m",
          "memory": "512Mi",
          "ephemeralStorage": "1Gi"
        }
      }
    }
  },
  "activeDeadlineSeconds": 3600,
  "network": {
    "allowInternetEgress": false,
    "allowClusterDNS": false,
    "allowedEgressCIDRs": [],
    "nameservers": []
  }
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `podTemplate.container.image` | string | Yes | Container image |
| `podTemplate.container.command` | string[] | No | Entrypoint override. Defaults to `["sleep", "infinity"]` if omitted. |
| `podTemplate.container.env` | map | No | Environment variables (write-only, not returned in responses) |
| `podTemplate.container.resources` | object | No | Resource requests and limits |
| `activeDeadlineSeconds` | int | No | Max lifetime in seconds |
| `network` | object | No | Network isolation config (immutable after creation) |

### Response

**`201 Created`**

```json
{
  "id": "my-sandbox-abc123",
  "podTemplate": {
    "container": {
      "image": "python:3.12",
      "command": ["/bin/bash"],
      "resources": { ... }
    }
  },
  "status": "creating",
  "creationTimestamp": "2026-02-27T10:00:00Z",
  "activeDeadlineSeconds": 3600,
  "network": { ... }
}
```

:::note
The response's `ContainerInfo` omits `env` to prevent leaking secrets. Only `image`, `command`, and `resources` are returned.
:::

---

## List Sandboxes

```
GET /sandboxes
```

### Response

**`200 OK`**

```json
{
  "sandboxes": [
    {
      "id": "my-sandbox-abc123",
      "status": "running",
      "creationTimestamp": "2026-02-27T10:00:00Z"
    }
  ]
}
```

The list endpoint returns summaries (id, status, creation timestamp) rather than full sandbox details. Use [Get Sandbox](#get-sandbox) for complete information.

---

## Get Sandbox

```
GET /sandboxes/{id}
```

### Parameters

| Parameter | Location | Type | Description |
|-----------|----------|------|-------------|
| `id` | path | string | Sandbox identifier |

### Response

**`200 OK`**

```json
{
  "id": "my-sandbox-abc123",
  "podTemplate": {
    "container": {
      "image": "python:3.12",
      "command": ["/bin/bash"],
      "resources": {
        "requests": { "cpu": "100m", "memory": "256Mi" },
        "limits": { "cpu": "500m", "memory": "512Mi" }
      }
    }
  },
  "status": "running",
  "creationTimestamp": "2026-02-27T10:00:00Z",
  "activeDeadlineSeconds": 3600,
  "network": {
    "allowInternetEgress": false
  }
}
```

### Status Values

| Status | Description |
|--------|-------------|
| `creating` | Pod is being scheduled and containers are starting |
| `running` | Pod is ready and accepting commands |
| `shuttingDown` | Sandbox is being deleted or snapshotted |
| `failed` | Pod encountered an error |
| `stopped` | Pod has exited |
| `unknown` | Status cannot be determined |

---

## Delete a Sandbox

```
DELETE /sandboxes/{id}
```

This operation is **idempotent** — deleting an already-deleted sandbox returns success.

### Parameters

| Parameter | Location | Type | Description |
|-----------|----------|------|-------------|
| `id` | path | string | Sandbox identifier |

### Response

**`204 No Content`**
