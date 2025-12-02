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

## Endpoints

### System

#### Health Check
```http
GET /health
```
- **Description**: Check if the API is running and healthy
- **Tags**: `system`
- **Authentication**: None required
- **Responses**:
  - `200 OK`: Service is healthy
  - `503 Service Unavailable`: Service is unhealthy

---

### Sandboxes

#### List Sandboxes
```http
GET /sandboxes
```
- **Description**: Returns a list of all sandboxes
- **Tags**: `sandboxes`
- **Operation ID**: `listSandboxes`
- **Query Parameters**:
  - `limit` (integer, optional): Maximum number of items to return (default: 20)
  - `offset` (integer, optional): Number of items to skip (default: 0)
- **Response**: `SandboxList`
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
- **Status Codes**:
  - `200 OK`: List of sandboxes
  - `401 Unauthorized`: Invalid or missing API key

#### Create Sandbox
```http
POST /sandboxes
```
- **Description**: Creates a new sandbox with the specified configuration
- **Tags**: `sandboxes`
- **Operation ID**: `createSandbox`
- **Request Body**: `CreateSandbox`
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
- **Response**: `Sandbox` (see structure above)
- **Status Codes**:
  - `201 Created`: Sandbox created successfully
  - `400 Bad Request`: Invalid request data
  - `401 Unauthorized`: Invalid or missing API key

#### Get Sandbox Details
```http
GET /sandboxes/{sandbox_id}
```
- **Description**: Returns detailed information about a specific sandbox
- **Tags**: `sandboxes`
- **Operation ID**: `getSandbox`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Response**: `Sandbox` (see structure above)
- **Status Codes**:
  - `200 OK`: Sandbox details
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found

#### Delete Sandbox
```http
DELETE /sandboxes/{sandbox_id}
```
- **Description**: Permanently deletes a sandbox and all associated resources
- **Tags**: `sandboxes`
- **Operation ID**: `deleteSandbox`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Query Parameters**:
  - `force` (boolean, optional): Force delete even if running (default: false)
- **Status Codes**:
  - `204 No Content`: Sandbox deleted successfully
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found
  - `409 Conflict`: Cannot delete running sandbox without force

#### Stop Sandbox
```http
POST /sandboxes/{sandbox_id}/stop
```
- **Description**: Stops a running sandbox
- **Tags**: `sandboxes`
- **Operation ID**: `stopSandbox`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Response**: `Sandbox` (updated state)
- **Status Codes**:
  - `202 Accepted`: Stop initiated
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found
  - `409 Conflict`: Sandbox is not running

#### Restart Sandbox
```http
POST /sandboxes/{sandbox_id}/restart
```
- **Description**: Restarts a sandbox (stop and start)
- **Tags**: `sandboxes`
- **Operation ID**: `restartSandbox`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Response**: `Sandbox` (updated state)
- **Status Codes**:
  - `202 Accepted`: Restart initiated
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found
  - `501 Not Implemented`: Only available for Kubernetes backend

---

### Execution

#### Execute Command
```http
POST /sandboxes/{sandbox_id}/execute
```
- **Description**: Execute a command in the specified sandbox
- **Tags**: `execution`
- **Operation ID**: `executeCommand`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Request Body**: `ExecuteCommandRequest`
  ```json
  {
    "command": "echo 'Hello, World!'"
  }
  ```
- **Response**: `ExecuteCommandResponse`
  ```json
  {
    "stdout": "Hello, World!\n",
    "stderr": "",
    "exitCode": 0
  }
  ```
- **Status Codes**:
  - `200 OK`: Command executed successfully
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found
  - `409 Conflict`: Sandbox not in started state
  - `501 Not Implemented`: Only available for Kubernetes backend

---

### Files

#### Upload File
```http
POST /sandboxes/{sandbox_id}/fs/upload
```
- **Description**: Upload a text file to the specified path in the sandbox
- **Tags**: `files`
- **Operation ID**: `uploadFile`
- **Path Parameters**:
  - `sandbox_id` (string, required): The sandbox ID
- **Request Body**: `FileUploadRequest`
  ```json
  {
    "path": "/home/user/script.py",
    "content": "print('Hello from Isola!')"
  }
  ```
- **Response**: `FileUploadResponse`
  ```json
  {
    "path": "/home/user/script.py",
    "size": 26
  }
  ```
- **Status Codes**:
  - `200 OK`: File uploaded successfully
  - `401 Unauthorized`: Invalid or missing API key
  - `404 Not Found`: Sandbox not found
  - `409 Conflict`: Sandbox not in started state
  - `501 Not Implemented`: Only available for Kubernetes backend

---

## Data Models

### SandboxState (Enum)
- `creating`: Sandbox is being created
- `starting`: Sandbox is starting
- `started`: Sandbox is running
- `stopping`: Sandbox is stopping
- `stopped`: Sandbox is stopped
- `destroying`: Sandbox is being destroyed
- `destroyed`: Sandbox has been destroyed
- `error`: Sandbox encountered an error
- `unknown`: State is unknown

### SandboxClass (Enum)
- `small`: 1 CPU, 1GB RAM, 10GB disk
- `medium`: 2 CPU, 2GB RAM, 20GB disk
- `large`: 4 CPU, 4GB RAM, 40GB disk
- `xlarge`: 8 CPU, 8GB RAM, 80GB disk

### Sandbox
```typescript
{
  id: string
  name: string
  state: SandboxState
  desiredState?: SandboxState
  class: SandboxClass
  region: string
  image?: string
  cpu?: number
  memory?: number
  disk?: number
  gpu: number
  env: Record<string, string>
  labels: Record<string, string>
  volumes: AttachedVolume[]
  ports: ExposedPort[]
  runnerId?: string
  errorReason?: string
  ipAddress?: string
  createdAt: DateTime
  updatedAt: DateTime
  lastActivityAt?: DateTime
}
```

### CreateSandbox
```typescript
{
  name: string
  image?: string
  class?: SandboxClass = "small"
  region?: string = "default"
  cpu?: number
  memory?: number
  disk?: number
  gpu?: number = 0
  env?: Record<string, string>
  labels?: Record<string, string>
  volumes?: AttachedVolume[]
  autoStart?: boolean = true
}
```

### ExecuteCommandRequest
```typescript
{
  command: string
}
```

### ExecuteCommandResponse
```typescript
{
  stdout: string
  stderr: string
  exitCode: number
}
```

### FileUploadRequest
```typescript
{
  path: string      // Absolute path in sandbox
  content: string   // Plain text content only
}
```

### FileUploadResponse
```typescript
{
  path: string
  size: number      // Size in bytes
}
```

---

## Example Usage

### Complete Workflow
```bash
# 1. Create a sandbox
curl -X POST http://localhost:3000/sandboxes \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dev-sandbox",
    "image": "python:3.11",
    "class": "small"
  }'
# Response: {"id": "abc-123", "state": "creating", ...}

# 2. Wait for sandbox to be ready
curl http://localhost:3000/sandboxes/abc-123 \
  -H "X-API-Key: iso_sk_demo"
# Response: {"id": "abc-123", "state": "started", ...}

# 3. Upload a file
curl -X POST http://localhost:3000/sandboxes/abc-123/fs/upload \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/home/user/hello.py",
    "content": "print(\"Hello, Isola!\")"
  }'

# 4. Execute the file
curl -X POST http://localhost:3000/sandboxes/abc-123/execute \
  -H "X-API-Key: iso_sk_demo" \
  -H "Content-Type: application/json" \
  -d '{"command": "python /home/user/hello.py"}'
# Response: {"stdout": "Hello, Isola!\n", "stderr": "", "exitCode": 0}

# 5. Clean up
curl -X DELETE http://localhost:3000/sandboxes/abc-123 \
  -H "X-API-Key: iso_sk_demo"
```

---

## Notes

1. **Backend Support**: Some features (execute, file upload, restart) are only available when using the Kubernetes backend (`SANDBOX_BACKEND=kubernetes`)

2. **Authentication**: The demo API key is `iso_sk_demo`. In production, implement proper API key management.

3. **Rate Limiting**: Not currently implemented but recommended for production use.

4. **WebSockets**: The controller also exposes a WebSocket endpoint at `/ws` for agent connections (internal use).

5. **OpenAPI/Swagger**: The full OpenAPI specification is available at `/openapi.json` and interactive documentation at `/docs`.
