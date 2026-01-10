# API Reference

Complete REST API documentation for the Isola Gateway. All endpoints are prefixed with `/api/v1`.

---

## Authentication

All API requests require an API key in the `X-API-Key` header:

```bash
curl -H "X-API-Key: your-api-key" https://api.isola.run/api/v1/sandboxes
```

---

## Base URL

| Environment | URL |
|-------------|-----|
| Local Development | `http://localhost:8080` |
| Production | Configure in your deployment |

---

## Response Format

All responses are JSON with consistent structure:

**Success:**
```json
{
  "id": "uuid",
  "name": "sandbox-name",
  ...
}
```

**Error:**
```json
{
  "error": "Error message describing what went wrong",
  "code": "ERROR_CODE"
}
```

---

## Endpoints Overview

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | [/sandboxes](#list-sandboxes) | List all sandboxes |
| `POST` | [/sandboxes](#create-sandbox) | Create a new sandbox |
| `GET` | [/sandboxes/:id](#get-sandbox) | Get sandbox details |
| `DELETE` | [/sandboxes/:id](#delete-sandbox) | Terminate a sandbox |
| `POST` | [/sandboxes/:id/execute](#execute-command) | Execute a command |
| `POST` | [/sandboxes/:id/files](#upload-file) | Upload a file (direct) |
| `POST` | [/sandboxes/:id/files/upload-url](#get-upload-url) | Get presigned upload URL |
| `POST` | [/sandboxes/:id/files/confirm](#confirm-upload) | Confirm S3 upload |
| `GET` | [/health](#health-check) | Health check |
| `GET` | [/ready](#readiness-check) | Readiness check |

---

## Sandboxes

### List Sandboxes

Retrieve all sandboxes for the authenticated tenant.

```
GET /api/v1/sandboxes
```

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `state` | string | Filter by state: `pending`, `running`, `stopped`, `error` |
| `label` | string | Filter by label (format: `key=value`) |

**Example Request:**

```bash
curl -X GET "http://localhost:8080/api/v1/sandboxes" \
  -H "X-API-Key: $API_KEY"
```

**Example Response:**

```json
{
  "sandboxes": [
    {
      "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "name": "python-sandbox-1",
      "state": "running",
      "templateName": "python-dev",
      "createdAt": "2025-01-10T10:00:00Z",
      "labels": {
        "team": "platform"
      }
    },
    {
      "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
      "name": "nodejs-sandbox-1",
      "state": "pending",
      "templateName": "nodejs-18",
      "createdAt": "2025-01-10T10:05:00Z",
      "labels": {}
    }
  ]
}
```

---

### Create Sandbox

Create a new sandbox instance.

```
POST /api/v1/sandboxes
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique sandbox name |
| `templateName` | string | No | SandboxTemplate to use |
| `image` | string | No | Override container image |
| `autoStart` | boolean | No | Start immediately (default: `true`) |
| `env` | object | No | Environment variables |
| `labels` | object | No | Custom labels |

**Example Request:**

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-sandbox",
    "templateName": "python-dev",
    "autoStart": true,
    "env": {
      "DEBUG": "true",
      "LOG_LEVEL": "info"
    },
    "labels": {
      "team": "platform",
      "purpose": "testing"
    }
  }'
```

**Example Response:**

```json
{
  "id": "c3d4e5f6-a7b8-9012-cdef-123456789012",
  "name": "my-sandbox",
  "state": "pending",
  "templateName": "python-dev",
  "createdAt": "2025-01-10T10:10:00Z",
  "labels": {
    "team": "platform",
    "purpose": "testing"
  }
}
```

**Interactive Example - Create and Wait:**

```bash
#!/bin/bash
# Create sandbox and wait for it to be ready

API="http://localhost:8080"
API_KEY="iso_sk_demo"

# Create
RESPONSE=$(curl -s -X POST "$API/api/v1/sandboxes" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "interactive-demo", "templateName": "python-dev", "autoStart": true}')

ID=$(echo $RESPONSE | jq -r '.id')
echo "Created sandbox: $ID"

# Poll until running
while true; do
  STATE=$(curl -s "$API/api/v1/sandboxes/$ID" \
    -H "X-API-Key: $API_KEY" | jq -r '.state')

  echo "State: $STATE"

  if [ "$STATE" = "running" ]; then
    echo "Sandbox is ready!"
    break
  elif [ "$STATE" = "error" ]; then
    echo "Sandbox failed!"
    exit 1
  fi

  sleep 2
done
```

---

### Get Sandbox

Retrieve details for a specific sandbox.

```
GET /api/v1/sandboxes/:id
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Sandbox UUID |

**Example Request:**

```bash
curl -X GET "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID" \
  -H "X-API-Key: $API_KEY"
```

**Example Response:**

```json
{
  "id": "c3d4e5f6-a7b8-9012-cdef-123456789012",
  "name": "my-sandbox",
  "state": "running",
  "templateName": "python-dev",
  "createdAt": "2025-01-10T10:10:00Z",
  "startedAt": "2025-01-10T10:10:15Z",
  "timeoutAt": "2025-01-10T11:10:15Z",
  "labels": {
    "team": "platform"
  },
  "podName": "my-sandbox-xyz123",
  "conditions": [
    {
      "type": "Ready",
      "status": "True",
      "lastTransitionTime": "2025-01-10T10:10:15Z"
    },
    {
      "type": "PodReady",
      "status": "True"
    },
    {
      "type": "NetworkConfigured",
      "status": "True"
    }
  ]
}
```

---

### Delete Sandbox

Terminate and delete a sandbox.

```
DELETE /api/v1/sandboxes/:id
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Sandbox UUID |

**Example Request:**

```bash
curl -X DELETE "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID" \
  -H "X-API-Key: $API_KEY"
```

**Example Response:**

```json
{
  "success": true,
  "message": "Sandbox termination initiated"
}
```

---

## Command Execution

### Execute Command

Execute a command inside a running sandbox.

```
POST /api/v1/sandboxes/:id/execute
```

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `id` | string | Sandbox UUID |

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | Yes | Command to execute |
| `timeout` | int | No | Timeout in seconds (default: 30) |
| `workdir` | string | No | Working directory |

**Example Request:**

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID/execute" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "command": "python -c \"import sys; print(sys.version)\"",
    "timeout": 60
  }'
```

**Example Response:**

```json
{
  "stdout": "3.11.4 (main, Jun  9 2023, 07:30:55) [GCC 12.2.0]\n",
  "stderr": "",
  "exitCode": 0
}
```

**Interactive Example - Multi-step Execution:**

```bash
#!/bin/bash
# Execute multiple commands in sequence

API="http://localhost:8080"
API_KEY="iso_sk_demo"
ID="your-sandbox-id"

execute() {
  curl -s -X POST "$API/api/v1/sandboxes/$ID/execute" \
    -H "X-API-Key: $API_KEY" \
    -H "Content-Type: application/json" \
    -d "{\"command\": \"$1\"}"
}

# Install a package
echo "Installing requests..."
execute "pip install requests" | jq

# Write a script
execute "cat > /workspace/test.py << 'EOF'
import requests
r = requests.get('https://httpbin.org/get')
print(f'Status: {r.status_code}')
EOF"

# Run the script
echo "Running script..."
execute "python /workspace/test.py" | jq '.stdout'
```

---

## File Operations

### Upload File (Direct)

Upload a file directly to the sandbox (recommended for files < 5MB).

```
POST /api/v1/sandboxes/:id/files
```

**Request:** `multipart/form-data`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | file | Yes | File to upload |
| `path` | string | Yes | Destination path in sandbox |

**Example Request:**

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID/files" \
  -H "X-API-Key: $API_KEY" \
  -F "file=@local-script.py" \
  -F "path=/workspace/script.py"
```

**Example Response:**

```json
{
  "success": true,
  "path": "/workspace/script.py",
  "size": 1234
}
```

---

### Get Upload URL

Get a presigned S3 URL for uploading large files (> 5MB).

```
POST /api/v1/sandboxes/:id/files/upload-url
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `filename` | string | Yes | Name of the file |
| `contentType` | string | No | MIME type (default: `application/octet-stream`) |

**Example Request:**

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID/files/upload-url" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "large-dataset.tar.gz",
    "contentType": "application/gzip"
  }'
```

**Example Response:**

```json
{
  "uploadId": "upload-abc123",
  "uploadUrl": "https://s3.amazonaws.com/bucket/path?X-Amz-...",
  "expiresAt": "2025-01-10T11:00:00Z"
}
```

---

### Confirm Upload

After uploading to S3, confirm the upload to trigger download to sandbox.

```
POST /api/v1/sandboxes/:id/files/confirm
```

**Request Body:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `uploadId` | string | Yes | Upload ID from upload-url response |
| `path` | string | Yes | Destination path in sandbox |

**Example Request:**

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$SANDBOX_ID/files/confirm" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "uploadId": "upload-abc123",
    "path": "/workspace/large-dataset.tar.gz"
  }'
```

**Example Response:**

```json
{
  "success": true,
  "path": "/workspace/large-dataset.tar.gz"
}
```

**Interactive Example - Large File Upload:**

```bash
#!/bin/bash
# Upload a large file via S3

API="http://localhost:8080"
API_KEY="iso_sk_demo"
ID="your-sandbox-id"
FILE="large-file.tar.gz"

# Step 1: Get presigned URL
UPLOAD=$(curl -s -X POST "$API/api/v1/sandboxes/$ID/files/upload-url" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"filename\": \"$FILE\"}")

UPLOAD_URL=$(echo $UPLOAD | jq -r '.uploadUrl')
UPLOAD_ID=$(echo $UPLOAD | jq -r '.uploadId')

echo "Upload ID: $UPLOAD_ID"

# Step 2: Upload to S3
echo "Uploading to S3..."
curl -X PUT "$UPLOAD_URL" \
  -H "Content-Type: application/gzip" \
  --data-binary "@$FILE"

# Step 3: Confirm upload
echo "Confirming upload..."
curl -s -X POST "$API/api/v1/sandboxes/$ID/files/confirm" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"uploadId\": \"$UPLOAD_ID\", \"path\": \"/workspace/$FILE\"}" | jq
```

---

## Health Checks

### Health Check

Simple health check endpoint.

```
GET /health
```

**Example Response:**

```json
{
  "status": "healthy"
}
```

---

### Readiness Check

Readiness check (Kubernetes probe).

```
GET /ready
```

**Example Response:**

```json
{
  "status": "ready",
  "kubernetes": "connected",
  "storage": "connected"
}
```

---

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `UNAUTHORIZED` | 401 | Invalid or missing API key |
| `FORBIDDEN` | 403 | Not authorized for this resource |
| `NOT_FOUND` | 404 | Sandbox not found |
| `CONFLICT` | 409 | Resource already exists |
| `INVALID_STATE` | 422 | Operation not allowed in current state |
| `TIMEOUT` | 504 | Command execution timeout |
| `INTERNAL_ERROR` | 500 | Internal server error |

**Example Error Response:**

```json
{
  "error": "Sandbox not found",
  "code": "NOT_FOUND"
}
```

---

## Rate Limits

| Endpoint | Limit |
|----------|-------|
| All endpoints | 100 requests/minute per API key |
| `/execute` | 30 requests/minute per sandbox |
| `/files` | 10 requests/minute per sandbox |

Rate limit headers:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1704888000
```

---

## SDKs and Client Libraries

### Python SDK

```python
from isola_client import IsolaClient

client = IsolaClient("http://localhost:8080", "your-api-key")

# Create sandbox
sandbox = client.create_sandbox(name="test", auto_start=True)

# Wait for ready
sandbox = client.wait_for_state(sandbox['id'], "running")

# Execute command
result = client.execute_command(sandbox['id'], "echo hello")

# Upload file
client.upload_file(sandbox['id'], "/workspace/file.txt", b"content")

# Cleanup
client.terminate_sandbox(sandbox['id'])
```

### cURL

All examples in this documentation use cURL. See individual endpoints for examples.
