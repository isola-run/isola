---
sidebar_position: 4
title: Filesystem
slug: /api/filesystem
---

# Filesystem API

## Upload a File

```
POST /sandboxes/{id}/filesystem?path=/workspace/file.txt
```

Streams a file upload to the specified path in the sandbox container.

### Parameters

| Parameter | Location | Type | Required | Description |
|-----------|----------|------|----------|-------------|
| `id` | path | string | Yes | Sandbox identifier |
| `path` | query | string | Yes | Destination path (absolute or relative to container cwd) |
| `container` | query | string | No | Container name. Defaults to the only container. |

### Request Body

Content-Type: `application/octet-stream`

Raw file bytes.

### Response

**`201 Created`**

```json
{
  "absolutePath": "/workspace/file.txt",
  "bytesWritten": 1024
}
```

---

## Download a File

```
GET /sandboxes/{id}/filesystem?path=/workspace/file.txt
```

Streams a file download from the specified path in the sandbox container.

### Parameters

| Parameter | Location | Type | Required | Description |
|-----------|----------|------|----------|-------------|
| `id` | path | string | Yes | Sandbox identifier |
| `path` | query | string | Yes | Source path (absolute or relative to container cwd) |
| `container` | query | string | No | Container name. Defaults to the only container. |

### Response

**`200 OK`**

Content-Type: `application/octet-stream`

Raw file bytes.

---

## Error Responses

| Code | Scenario |
|------|----------|
| `400` | Invalid path or request format |
| `404` | Sandbox not found, or file does not exist (download) |
| `409` | Sandbox is not in a running state |
| `502` | Error communicating with the sandbox sidecar |
