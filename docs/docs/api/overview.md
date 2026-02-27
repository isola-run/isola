---
sidebar_position: 1
title: API Overview
slug: /api/overview
---

# REST API Overview

The Isola API gateway provides a REST API for managing sandboxes, executing commands, and transferring files. It runs on port `8080` by default.

## Base URL

```
http://<api-gateway-host>:8080
```

## Authentication

The API gateway does not currently implement authentication. Access control should be handled at the network level (e.g., Kubernetes NetworkPolicies, ingress controllers, or service mesh).

## Endpoints

### Sandboxes

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/sandboxes` | [Create a sandbox](./sandboxes.md#create-a-sandbox) |
| `GET` | `/sandboxes` | [List sandboxes](./sandboxes.md#list-sandboxes) |
| `GET` | `/sandboxes/{id}` | [Get sandbox details](./sandboxes.md#get-sandbox) |
| `DELETE` | `/sandboxes/{id}` | [Delete a sandbox](./sandboxes.md#delete-a-sandbox) |

### Commands

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/sandboxes/{id}/commands` | [Start a command](./commands.md#start-a-command) |
| `GET` | `/sandboxes/{id}/commands/{cmdId}/status` | [Get exit code](./commands.md#get-command-status) |
| `GET` | `/sandboxes/{id}/commands/{cmdId}/stdout` | [Stream stdout](./commands.md#stream-stdout) |
| `GET` | `/sandboxes/{id}/commands/{cmdId}/stderr` | [Stream stderr](./commands.md#stream-stderr) |
| `POST` | `/sandboxes/{id}/commands/{cmdId}/stdin` | [Write to stdin](./commands.md#write-to-stdin) |
| `DELETE` | `/sandboxes/{id}/commands/{cmdId}` | [Kill a command](./commands.md#kill-a-command) |

### Filesystem

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/sandboxes/{id}/filesystem` | [Upload a file](./filesystem.md#upload-a-file) |
| `GET` | `/sandboxes/{id}/filesystem` | [Download a file](./filesystem.md#download-a-file) |

### Health

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/healthz` | Health check (alias) |
| `GET` | `/ready` | Readiness check |
| `GET` | `/readyz` | Readiness check (alias) |

## Error Responses

All errors follow the [RFC 7807](https://tools.ietf.org/html/rfc7807) Problem Details format:

```json
{
  "type": "about:blank",
  "title": "Bad Request",
  "status": 400,
  "detail": "Property foo is required but is missing.",
  "errors": [
    {
      "message": "expected required property foo to be present",
      "location": "body.foo"
    }
  ]
}
```

### Status Codes

| Code | Meaning |
|------|---------|
| `400` | Bad Request — validation error in the request body |
| `404` | Not Found — sandbox or command does not exist |
| `409` | Conflict — sandbox is not in a valid state for the operation |
| `422` | Unprocessable Entity — request is well-formed but semantically invalid |
| `500` | Internal Server Error — unexpected error in the API gateway |
| `502` | Bad Gateway — error communicating with the sandbox sidecar |

## Content Types

- Request/response bodies use `application/json`
- File uploads and downloads use `application/octet-stream`
- Stdout/stderr streams return `application/octet-stream`
- Error responses use `application/problem+json`
