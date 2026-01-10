# Sandbox

A **Sandbox** is the primary resource in Isola - it represents a running isolated environment where you can execute code, upload files, and run commands.

---

## Overview

Sandboxes are ephemeral Kubernetes pods with:

- **Isolation** - Network policies restrict traffic
- **Lifecycle management** - Automatic creation, monitoring, and cleanup
- **File system access** - Upload and download files
- **Command execution** - Run arbitrary commands

---

## Resource Definition

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: my-sandbox
  namespace: isola-sandboxes
  labels:
    app: my-app
    environment: development
spec:
  # Required: Reference to a SandboxTemplate
  templateRef:
    name: python-sandbox

  # Optional: Network configuration
  network:
    # Option A: Reference existing NetworkTemplate
    templateRef:
      name: egress-only

    # Option B: Embed network spec (creates owned template)
    # spec:
    #   allowedEgress:
    #     - "0.0.0.0/0"
    #   dnsServers:
    #     - "8.8.8.8"
```

---

## Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `templateRef.name` | string | Yes | Name of the SandboxTemplate to use |
| `network.templateRef.name` | string | No | Reference to existing NetworkTemplate |
| `network.spec` | NetworkTemplateSpec | No | Embedded network configuration |

**Note:** If neither `network.templateRef` nor `network.spec` is provided, the default `isola-isolated` network template is used (deny all traffic).

---

## Status Fields

```yaml
status:
  # Kubernetes conditions
  conditions:
    - type: Ready
      status: "True"
      reason: AllConditionsMet
      lastTransitionTime: "2025-01-10T10:00:00Z"
    - type: PodReady
      status: "True"
    - type: NetworkConfigured
      status: "True"

  # Timeout timestamp (computed from template's timeoutSeconds)
  timeoutAt: "2025-01-10T10:05:00Z"

  # Name of the created pod
  podName: my-sandbox-abc123

  # Effective network template name
  effectiveNetworkTemplate: egress-only
```

| Field | Description |
|-------|-------------|
| `conditions` | Array of Kubernetes conditions indicating sandbox health |
| `timeoutAt` | When the sandbox will auto-terminate |
| `podName` | Name of the underlying Kubernetes pod |
| `effectiveNetworkTemplate` | Resolved network template name |

---

## Conditions Reference

| Condition | True When |
|-----------|-----------|
| `Ready` | Sandbox is fully operational (all other conditions met, not timed out) |
| `PodReady` | The sandbox pod is running and healthy |
| `NetworkConfigured` | NetworkPolicy has been applied successfully |
| `TimedOut` | Sandbox has exceeded its timeout duration |
| `SnapshottingFilesystem` | Filesystem snapshot is in progress |

---

## Lifecycle

### Creation Flow

```
1. User creates Sandbox CR
                │
                ▼
2. Operator resolves SandboxTemplate
                │
                ▼
3. Operator resolves/creates NetworkTemplate
                │
                ▼
4. Operator creates Pod with agent sidecar
                │
                ▼
5. Operator creates NetworkPolicy
                │
                ▼
6. Conditions update as resources become ready
```

### Deletion Flow

```
1. User deletes Sandbox CR (or timeout reached)
                │
                ▼
2. Finalizer prevents immediate deletion
                │
                ▼
3. Shutdown policy executed (if configured)
                │
                ▼
4. Pod and NetworkPolicy deleted
                │
                ▼
5. Finalizer removed, CR deleted
```

---

## Examples

### Basic Sandbox

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: basic-sandbox
  namespace: isola-sandboxes
spec:
  templateRef:
    name: python-sandbox
```

### Sandbox with Custom Network

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: network-sandbox
  namespace: isola-sandboxes
spec:
  templateRef:
    name: python-sandbox
  network:
    spec:
      allowedEgress:
        - "10.0.0.0/8"      # Allow internal traffic
        - "8.8.8.8/32"       # Allow Google DNS
      dnsServers:
        - "8.8.8.8"
```

### Sandbox with Labels

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: labeled-sandbox
  namespace: isola-sandboxes
  labels:
    tenant: acme-corp
    purpose: testing
    team: platform
spec:
  templateRef:
    name: python-sandbox
```

---

## Creating via API

```bash
# Basic creation
curl -X POST "http://localhost:8080/api/v1/sandboxes" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "api-sandbox",
    "templateName": "python-sandbox",
    "autoStart": true
  }'

# With custom image and environment
curl -X POST "http://localhost:8080/api/v1/sandboxes" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "custom-sandbox",
    "image": "node:18-slim",
    "env": {
      "NODE_ENV": "development",
      "DEBUG": "true"
    },
    "autoStart": true
  }'
```

---

## Querying Sandboxes

### List All Sandboxes

```bash
kubectl get sandboxes -n isola-sandboxes

# Example output:
NAME              READY   POD-READY   AGE
basic-sandbox     True    True        5m
network-sandbox   True    True        2m
```

### Get Sandbox Details

```bash
kubectl describe sandbox basic-sandbox -n isola-sandboxes

# Or via API
curl -s "http://localhost:8080/api/v1/sandboxes/$ID" \
  -H "X-API-Key: $API_KEY" | jq
```

### Watch Sandbox Status

```bash
kubectl get sandbox basic-sandbox -n isola-sandboxes -w
```

---

## Operations on Running Sandboxes

Sandboxes in the `running` state support these operations:

### Execute Command

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/execute" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"command": "python --version"}'
```

### Upload File

```bash
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/files" \
  -H "X-API-Key: $API_KEY" \
  -F "file=@script.py" \
  -F "path=/workspace/script.py"
```

### Large File Upload (via S3)

```bash
# 1. Get presigned upload URL
URL=$(curl -s -X POST "http://localhost:8080/api/v1/sandboxes/$ID/files/upload-url" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"filename": "large-file.tar.gz"}' | jq -r '.uploadUrl')

# 2. Upload to S3
curl -X PUT "$URL" --data-binary @large-file.tar.gz

# 3. Confirm upload (triggers download to sandbox)
curl -X POST "http://localhost:8080/api/v1/sandboxes/$ID/files/confirm" \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"uploadId": "...", "path": "/workspace/large-file.tar.gz"}'
```

---

## Deleting Sandboxes

```bash
# Via kubectl
kubectl delete sandbox basic-sandbox -n isola-sandboxes

# Via API
curl -X DELETE "http://localhost:8080/api/v1/sandboxes/$ID" \
  -H "X-API-Key: $API_KEY"
```

The finalizer ensures cleanup completes before the resource is removed.

---

## Best Practices

1. **Use templates** - Define common configurations as SandboxTemplates for consistency
2. **Set appropriate timeouts** - Prevent runaway sandboxes from consuming resources
3. **Apply network isolation** - Use NetworkTemplates to restrict egress traffic
4. **Label sandboxes** - Add metadata for filtering and organization
5. **Handle errors gracefully** - Check sandbox state before executing operations
