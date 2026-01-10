# Getting Started

Create your first sandbox in 5 minutes. This guide walks you through installation, configuration, and running your first isolated workload.

---

## Prerequisites

- **Kubernetes cluster** (1.28+) - Local (Kind, Minikube) or cloud
- **kubectl** configured to access your cluster
- **Helm 3.x** for chart installation

---

## Installation

### Option 1: Local Development (Recommended for Testing)

```bash
# Clone the repository
git clone https://github.com/isola-ai/isola.git
cd isola

# Set up local Kind cluster with registry
./scripts/setup.sh

# Start the development environment
tilt up

# Access Tilt dashboard at http://localhost:10350
```

### Option 2: Helm Installation (Production)

```bash
# Add the Isola Helm repository
helm repo add isola https://charts.isola.run
helm repo update

# Install the operator
helm install isola-operator isola/isola-operator \
  --namespace isola-system \
  --create-namespace

# Install the gateway
helm install isola-gw isola/isola-gw \
  --namespace isola-system \
  --set auth.apiKey="your-secure-api-key"
```

---

## Quick Start

### Step 1: Create a Sandbox Template

Templates define the base configuration for sandboxes. Create a template for Python workloads:

```yaml
# python-template.yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 300  # Auto-terminate after 5 minutes
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11-slim
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
          workingDir: /workspace
```

Apply the template:

```bash
kubectl apply -f python-template.yaml
```

### Step 2: Create Your First Sandbox

#### Using the REST API

```bash
# Set your API endpoint and key
export ISOLA_API="http://localhost:8080"
export ISOLA_API_KEY="iso_sk_demo"

# Create a sandbox
curl -X POST "$ISOLA_API/api/v1/sandboxes" \
  -H "X-API-Key: $ISOLA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-first-sandbox",
    "templateName": "python-sandbox",
    "autoStart": true
  }'
```

**Response:**
```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "name": "my-first-sandbox",
  "state": "pending",
  "createdAt": "2025-01-10T10:00:00Z"
}
```

#### Using kubectl

```yaml
# my-sandbox.yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: my-first-sandbox
  namespace: isola-sandboxes
spec:
  templateRef:
    name: python-sandbox
```

```bash
kubectl apply -f my-sandbox.yaml
```

### Step 3: Wait for the Sandbox to be Ready

```bash
# Poll until state is "running"
curl -s "$ISOLA_API/api/v1/sandboxes/$SANDBOX_ID" \
  -H "X-API-Key: $ISOLA_API_KEY" | jq '.state'
```

Or watch the Kubernetes resource:

```bash
kubectl get sandbox my-first-sandbox -n isola-sandboxes -w
```

### Step 4: Execute Commands

Run Python code inside your sandbox:

```bash
curl -X POST "$ISOLA_API/api/v1/sandboxes/$SANDBOX_ID/execute" \
  -H "X-API-Key: $ISOLA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "command": "python -c \"print(sum(range(100)))\""
  }'
```

**Response:**
```json
{
  "stdout": "4950\n",
  "stderr": "",
  "exitCode": 0
}
```

### Step 5: Upload and Run Files

Upload a Python script:

```bash
# Create a test script
echo 'print("Hello from Isola!")' > hello.py

# Upload it
curl -X POST "$ISOLA_API/api/v1/sandboxes/$SANDBOX_ID/files" \
  -H "X-API-Key: $ISOLA_API_KEY" \
  -F "file=@hello.py" \
  -F "path=/workspace/hello.py"
```

Execute the uploaded script:

```bash
curl -X POST "$ISOLA_API/api/v1/sandboxes/$SANDBOX_ID/execute" \
  -H "X-API-Key: $ISOLA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"command": "python /workspace/hello.py"}'
```

### Step 6: Clean Up

Terminate the sandbox when done:

```bash
curl -X DELETE "$ISOLA_API/api/v1/sandboxes/$SANDBOX_ID" \
  -H "X-API-Key: $ISOLA_API_KEY"
```

Or via kubectl:

```bash
kubectl delete sandbox my-first-sandbox -n isola-sandboxes
```

---

## Using the Python SDK

For programmatic access, use the included Python client:

```python
from isola_client import IsolaClient

# Initialize the client
client = IsolaClient(
    base_url="http://localhost:8080",
    api_key="iso_sk_demo"
)

# Create and start a sandbox
sandbox = client.create_sandbox(
    name="python-demo",
    template_name="python-sandbox",
    auto_start=True
)
print(f"Created sandbox: {sandbox['id']}")

# Wait for it to be ready
sandbox = client.wait_for_state(sandbox['id'], "running", timeout=60)

# Execute code
result = client.execute_command(
    sandbox['id'],
    "python -c 'import sys; print(sys.version)'"
)
print(f"Python version: {result['stdout']}")

# Upload a file
client.upload_file(
    sandbox['id'],
    "/workspace/data.txt",
    b"Hello, World!"
)

# Execute with the uploaded file
result = client.execute_command(
    sandbox['id'],
    "cat /workspace/data.txt"
)
print(f"File contents: {result['stdout']}")

# Clean up
client.terminate_sandbox(sandbox['id'])
```

---

## Interactive Example: Data Processing Pipeline

Here's a complete example that creates a sandbox, processes data, and retrieves results:

```python
import json
from isola_client import IsolaClient

client = IsolaClient("http://localhost:8080", "iso_sk_demo")

# 1. Create sandbox
sandbox = client.create_sandbox(
    name="data-processor",
    template_name="python-sandbox",
    auto_start=True
)
sandbox = client.wait_for_state(sandbox['id'], "running")

# 2. Upload processing script
processing_script = '''
import json
import sys

# Read input data
data = json.loads(sys.argv[1])

# Process: calculate statistics
result = {
    "count": len(data),
    "sum": sum(data),
    "average": sum(data) / len(data),
    "min": min(data),
    "max": max(data)
}

print(json.dumps(result))
'''

client.upload_file(sandbox['id'], "/workspace/process.py", processing_script.encode())

# 3. Execute with sample data
sample_data = [10, 20, 30, 40, 50]
result = client.execute_command(
    sandbox['id'],
    f"python /workspace/process.py '{json.dumps(sample_data)}'"
)

# 4. Parse and display results
stats = json.loads(result['stdout'])
print(f"Statistics: {stats}")
# Output: Statistics: {"count": 5, "sum": 150, "average": 30.0, "min": 10, "max": 50}

# 5. Clean up
client.terminate_sandbox(sandbox['id'])
```

---

## What's Next?

- **[Core Concepts](./concepts/README.md)** - Understand templates, network isolation, and lifecycle
- **[API Reference](./api/README.md)** - Complete REST API documentation
- **[Tutorials](./tutorials/README.md)** - Step-by-step guides for common patterns
- **[Configuration](./configuration.md)** - Advanced template and network configuration

---

## Troubleshooting

### Sandbox stuck in "pending" state

Check if the operator is running:
```bash
kubectl get pods -n isola-system -l app=isola-operator
```

Check operator logs:
```bash
kubectl logs -n isola-system -l app=isola-operator
```

### "Sandbox not found" errors

Ensure you're using the correct sandbox ID (UUID) not the name:
```bash
# List all sandboxes to get IDs
curl -s "$ISOLA_API/api/v1/sandboxes" -H "X-API-Key: $ISOLA_API_KEY" | jq
```

### Command execution timeout

Increase the timeout in your template or API request. Default is 30 seconds:
```json
{
  "command": "long-running-script.sh",
  "timeout": 120
}
```

### Network connectivity issues

Check if your sandbox has network access:
```bash
# Execute a connectivity test
curl -X POST "$ISOLA_API/api/v1/sandboxes/$ID/execute" \
  -H "X-API-Key: $ISOLA_API_KEY" \
  -d '{"command": "curl -I https://example.com"}'
```

If blocked, check your NetworkTemplate. Default policy blocks all traffic.
