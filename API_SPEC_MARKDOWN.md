# Isola API Specification

## Base URL

```
http://localhost:3000  # Docker Compose
http://localhost:30080 # Minikube NodePort
```

## Authentication

All endpoints (except `/health`) require an API key header:
```
X-API-Key: iso_sk_demo
```

---

## 📚 API Endpoints

### System

#### Health Check

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Check if the API is running and healthy |

**Authentication:** None required  
**Tags:** `system`

**Responses:**
- `200 OK` - Service is healthy
- `503 Service Unavailable` - Service is unhealthy

---

### Sandboxes

#### List Sandboxes

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/sandboxes` | Returns a list of all sandboxes |

**Tags:** `sandboxes`  
**Operation ID:** `listSandboxes`

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `limit` | integer | No | Maximum number of items to return (default: 20) |
| `offset` | integer | No | Number of items to skip (default: 0) |

**Response:** `SandboxList`
```json
{
  "items": [
    {
      "id": "string",
      "name": "string",
      "state": "creating|starting|started|stopping|stopped|destroying|destroyed|error|unknown",
      "desiredState": "string",
      "class": "small|medium|large|xlarge",
      "region": "string",
      "image": "string",
      "cpu": 1,
      "memory": 1,
      "disk": 10,
      "gpu": 0,
      "env": {},
      "labels": {},
      "volumes": [],
      "ports": [],
      "runnerId": "string",
      "errorReason": "string",
      "ipAddress": "string",
      "createdAt": "2025-11-10T12:00:00",
      "updatedAt": "2025-11-10T12:00:00",
      "lastActivityAt": "2025-11-10T12:00:00"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

**Status Codes:**
- `200 OK` - List of sandboxes
- `401 Unauthorized` - Invalid or missing API key

---

#### Create Sandbox

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/sandboxes` | Creates a new sandbox with the specified configuration |

**Tags:** `sandboxes`  
**Operation ID:** `createSandbox`

**Request Body:** `CreateSandbox`
```json
{
  "name": "my-sandbox",
  "image": "python:3.11",
  "class": "small",
  "region": "default",
  "cpu": 1,
  "memory": 1,
  "disk": 10,
  "gpu": 0,
  "env": {
    "KEY": "value"
  },
  "labels": {
    "project": "test"
  },
  "volumes": [],
  "autoStart": true
}
```

**Response:** `Sandbox`

**Status Codes:**
- `201 Created` - Sandbox created successfully
- `400 Bad Request` - Invalid request data
- `401 Unauthorized` - Invalid or missing API key

---

#### Get Sandbox Details

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/sandboxes/{sandbox_id}` | Returns detailed information about a specific sandbox |

**Tags:** `sandboxes`  
**Operation ID:** `getSandbox`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Response:** `Sandbox`

**Status Codes:**
- `200 OK` - Sandbox details
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found

---

#### Delete Sandbox

| Method | Endpoint | Description |
|--------|----------|-------------|
| `DELETE` | `/sandboxes/{sandbox_id}` | Permanently deletes a sandbox and all associated resources |

**Tags:** `sandboxes`  
**Operation ID:** `deleteSandbox`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `force` | boolean | No | Force delete even if running (default: false) |

**Status Codes:**
- `204 No Content` - Sandbox deleted successfully
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found
- `409 Conflict` - Cannot delete running sandbox without force

---

#### Stop Sandbox

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/sandboxes/{sandbox_id}/stop` | Stops a running sandbox |

**Tags:** `sandboxes`  
**Operation ID:** `stopSandbox`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Response:** `Sandbox` (updated state)

**Status Codes:**
- `202 Accepted` - Stop initiated
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found
- `409 Conflict` - Sandbox is not running

---

#### Restart Sandbox

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/sandboxes/{sandbox_id}/restart` | Restarts a sandbox (stop and start) |

**Tags:** `sandboxes`  
**Operation ID:** `restartSandbox`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Response:** `Sandbox` (updated state)

**Status Codes:**
- `202 Accepted` - Restart initiated
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found
- `501 Not Implemented` - Only available for Kubernetes backend

---

### Execution

#### Execute Command

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/sandboxes/{sandbox_id}/execute` | Execute a command in the specified sandbox |

**Tags:** `execution`  
**Operation ID:** `executeCommand`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Request Body:** `ExecuteCommandRequest`
```json
{
  "command": "echo 'Hello, World!'"
}
```

**Response:** `ExecuteCommandResponse`
```json
{
  "stdout": "Hello, World!\n",
  "stderr": "",
  "exitCode": 0
}
```

**Status Codes:**
- `200 OK` - Command executed successfully
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found
- `409 Conflict` - Sandbox not in started state
- `501 Not Implemented` - Only available for Kubernetes backend

---

### Files

#### Upload File

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/sandboxes/{sandbox_id}/fs/upload` | Upload a text file to the specified path in the sandbox |

**Tags:** `files`  
**Operation ID:** `uploadFile`

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `sandbox_id` | string | Yes | The sandbox ID |

**Request Body:** `FileUploadRequest`
```json
{
  "path": "/home/user/script.py",
  "content": "print('Hello from Isola!')"
}
```

**Response:** `FileUploadResponse`
```json
{
  "path": "/home/user/script.py",
  "size": 26
}
```

**Status Codes:**
- `200 OK` - File uploaded successfully
- `401 Unauthorized` - Invalid or missing API key
- `404 Not Found` - Sandbox not found
- `409 Conflict` - Sandbox not in started state
- `501 Not Implemented` - Only available for Kubernetes backend

---

## 📦 Data Models

### SandboxState (Enum)

| Value | Description |
|-------|-------------|
| `creating` | Sandbox is being created |
| `starting` | Sandbox is starting |
| `started` | Sandbox is running |
| `stopping` | Sandbox is stopping |
| `stopped` | Sandbox is stopped |
| `destroying` | Sandbox is being destroyed |
| `destroyed` | Sandbox has been destroyed |
| `error` | Sandbox encountered an error |
| `unknown` | State is unknown |

### SandboxClass (Enum)

| Value | Resources |
|-------|-----------|
| `small` | 1 CPU, 1GB RAM, 10GB disk |
| `medium` | 2 CPU, 2GB RAM, 20GB disk |
| `large` | 4 CPU, 4GB RAM, 40GB disk |
| `xlarge` | 8 CPU, 8GB RAM, 80GB disk |

### Sandbox Model

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier |
| `name` | string | Sandbox name |
| `state` | SandboxState | Current state |
| `desiredState` | SandboxState | Target state (optional) |
| `class` | SandboxClass | Size class |
| `region` | string | Region/location |
| `image` | string | Container image (optional) |
| `cpu` | number | CPU cores (optional) |
| `memory` | number | Memory in GB (optional) |
| `disk` | number | Disk in GB (optional) |
| `gpu` | number | GPU count |
| `env` | object | Environment variables |
| `labels` | object | Metadata labels |
| `volumes` | array | Attached volumes |
| `ports` | array | Exposed ports |
| `runnerId` | string | Runner ID (optional) |
| `errorReason` | string | Error details (optional) |
| `ipAddress` | string | IP address (optional) |
| `createdAt` | DateTime | Creation timestamp |
| `updatedAt` | DateTime | Last update timestamp |
| `lastActivityAt` | DateTime | Last activity (optional) |

### CreateSandbox Model

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | _required_ | Sandbox name |
| `image` | string | python:3.11 | Container image |
| `class` | SandboxClass | small | Size class |
| `region` | string | default | Region/location |
| `cpu` | number | _auto_ | CPU cores |
| `memory` | number | _auto_ | Memory in GB |
| `disk` | number | _auto_ | Disk in GB |
| `gpu` | number | 0 | GPU count |
| `env` | object | {} | Environment variables |
| `labels` | object | {} | Metadata labels |
| `volumes` | array | [] | Volume attachments |
| `autoStart` | boolean | true | Start automatically |

---

## 🚀 Example Usage

### Complete Workflow

#### Step 1: Create a sandbox
```bash
curl -X POST http://localhost:3000/sandboxes \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dev-sandbox",
    "image": "python:3.11",
    "class": "small"
  }'
```

**Response:**
```json
{"id": "abc-123", "state": "creating", ...}
```

#### Step 2: Wait for sandbox to be ready
```bash
curl http://localhost:3000/sandboxes/abc-123 \
  -H "X-API-Key: iso_sk_demo"
```

**Response:**
```json
{"id": "abc-123", "state": "started", ...}
```

#### Step 3: Upload a file
```bash
curl -X POST http://localhost:3000/sandboxes/abc-123/fs/upload \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/home/user/hello.py",
    "content": "print(\"Hello, Isola!\")"
  }'
```

#### Step 4: Execute the file
```bash
curl -X POST http://localhost:3000/sandboxes/abc-123/execute \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{"command": "python /home/user/hello.py"}'
```

**Response:**
```json
{"stdout": "Hello, Isola!\n", "stderr": "", "exitCode": 0}
```

#### Step 5: Clean up
```bash
curl -X DELETE http://localhost:3000/sandboxes/abc-123 \
  -H "X-API-Key: iso_sk_demo"
```

---

## ⚠️ Important Notes

> **Backend Support**  
> Some features (execute, file upload, restart) are only available when using the Kubernetes backend (`SANDBOX_BACKEND=kubernetes`)

> **Authentication**  
> The demo API key is `iso_sk_demo`. In production, implement proper API key management.

> **Rate Limiting**  
> Not currently implemented but recommended for production use.

> **WebSockets**  
> The controller also exposes a WebSocket endpoint at `/ws` for agent connections (internal use).

> **OpenAPI/Swagger**  
> The full OpenAPI specification is available at `/openapi.json` and interactive documentation at `/docs`
