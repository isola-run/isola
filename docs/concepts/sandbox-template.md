# SandboxTemplate

A **SandboxTemplate** defines reusable configuration for sandbox pods, including container images, resource limits, timeouts, and shutdown policies.

---

## Overview

Templates enable:

- **Consistency** - All sandboxes from a template have identical base configuration
- **Reusability** - Define once, use many times
- **Separation of concerns** - Platform teams define templates, users create sandboxes

---

## Resource Definition

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-sandbox
  namespace: isola-sandboxes
spec:
  # Timeout before auto-termination (seconds)
  timeoutSeconds: 300

  # What happens when sandbox terminates
  shutdownPolicy:
    policy: Delete  # or SnapshotFilesystem

  # Full Kubernetes PodTemplateSpec
  podTemplate:
    metadata:
      labels:
        sandbox-type: python
    spec:
      containers:
        - name: sandbox
          image: python:3.11-slim
          command: ["sleep", "infinity"]
          workingDir: /workspace
          env:
            - name: PYTHONUNBUFFERED
              value: "1"
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir: {}
```

---

## Spec Fields

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `timeoutSeconds` | int | No | 300 | Duration before auto-termination |
| `shutdownPolicy` | ShutdownPolicy | No | Delete | Action on termination |
| `podTemplate` | PodTemplateSpec | Yes | - | Kubernetes pod specification |

### ShutdownPolicy

| Field | Type | Values | Description |
|-------|------|--------|-------------|
| `policy` | string | `Delete`, `SnapshotFilesystem` | Termination behavior |

**Policy behaviors:**

- **Delete** - Immediately delete the pod (default)
- **SnapshotFilesystem** - Upload workspace to S3 before deletion

### PodTemplate

The `podTemplate` field accepts a standard Kubernetes `PodTemplateSpec`. Key fields:

| Field | Description |
|-------|-------------|
| `spec.containers[].image` | Container image to run |
| `spec.containers[].command` | Override the container entrypoint |
| `spec.containers[].resources` | CPU and memory limits |
| `spec.containers[].env` | Environment variables |
| `spec.containers[].volumeMounts` | Mount points for volumes |
| `spec.volumes` | Volume definitions |

---

## Examples

### Python Development Environment

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-dev
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 3600  # 1 hour
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11
          command: ["sleep", "infinity"]
          workingDir: /workspace
          env:
            - name: PYTHONUNBUFFERED
              value: "1"
            - name: PIP_NO_CACHE_DIR
              value: "1"
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
```

### Node.js Environment

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: nodejs-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 600
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: node:20-slim
          command: ["sleep", "infinity"]
          workingDir: /app
          env:
            - name: NODE_ENV
              value: "development"
          resources:
            requests:
              cpu: "200m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
```

### Minimal Isolated Environment

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: minimal-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 60  # Short-lived
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: alpine:latest
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: "50m"
              memory: "64Mi"
            limits:
              cpu: "100m"
              memory: "128Mi"
```

### With Filesystem Snapshot

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: snapshot-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 1800  # 30 minutes
  shutdownPolicy:
    policy: SnapshotFilesystem
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11
          command: ["sleep", "infinity"]
          workingDir: /workspace
          volumeMounts:
            - name: workspace
              mountPath: /workspace
      volumes:
        - name: workspace
          emptyDir:
            sizeLimit: 1Gi  # Limit workspace size
```

### With GPU Support

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: gpu-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 7200  # 2 hours
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: nvidia/cuda:12.0-runtime-ubuntu22.04
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: "1000m"
              memory: "4Gi"
              nvidia.com/gpu: "1"
            limits:
              cpu: "4000m"
              memory: "16Gi"
              nvidia.com/gpu: "1"
      # May require node selector or tolerations
      nodeSelector:
        gpu: "true"
```

---

## Resource Limits Best Practices

### Choosing Appropriate Limits

| Use Case | CPU Request | CPU Limit | Memory Request | Memory Limit |
|----------|-------------|-----------|----------------|--------------|
| Lightweight scripts | 50m | 200m | 64Mi | 256Mi |
| Standard development | 100m | 500m | 128Mi | 512Mi |
| Data processing | 250m | 1000m | 256Mi | 2Gi |
| ML/AI workloads | 500m | 4000m | 1Gi | 8Gi |

### Important Considerations

1. **Requests vs Limits**
   - Requests = guaranteed resources
   - Limits = maximum allowed
   - Set requests lower than limits for burstable performance

2. **Memory limits are hard** - Exceeding memory limit kills the container (OOMKilled)

3. **CPU limits are soft** - Container gets throttled, not killed

---

## Agent Sidecar Injection

The operator automatically injects the `isola-agent` sidecar into every sandbox pod:

```yaml
# Injected by operator (do not include in template)
containers:
  - name: isola-agent
    image: isola-agent:latest
    ports:
      - containerPort: 8080
    env:
      - name: ISOLA_MAIN_CONTAINER
        value: "sandbox"  # Name of your main container
    securityContext:
      runAsUser: 0  # Required for /proc access
```

**Note:** The agent requires the main container to be named `sandbox` by convention.

---

## Template Selection Strategy

### Naming Conventions

```
{language}-{variant}
```

Examples:
- `python-dev` - Full Python development environment
- `python-slim` - Minimal Python for quick scripts
- `nodejs-18` - Node.js 18 LTS
- `golang-build` - Go with build tools

### Organizing Templates

```yaml
# Use labels for categorization
metadata:
  name: python-dev
  labels:
    language: python
    variant: development
    team: platform
```

Query templates by label:

```bash
kubectl get sandboxtemplates -n isola-sandboxes -l language=python
```

---

## Updating Templates

Templates can be updated, but changes only affect **new sandboxes**. Existing sandboxes continue using their original configuration.

```bash
# Update a template
kubectl apply -f updated-template.yaml

# Existing sandboxes unchanged
# New sandboxes use updated config
```

To apply updates to existing sandboxes:
1. Delete the sandbox
2. Create a new sandbox with the same template

---

## Validation

The operator validates templates at creation time:

| Validation | Description |
|------------|-------------|
| Container exists | At least one container must be defined |
| Image specified | Container must have an image |
| Resources valid | Limits must be >= requests |
| Timeout positive | timeoutSeconds must be > 0 |

Invalid templates are rejected with descriptive error messages.

---

## Listing Templates

```bash
# List all templates
kubectl get sandboxtemplates -n isola-sandboxes

# Get template details
kubectl describe sandboxtemplate python-dev -n isola-sandboxes

# Output template as YAML
kubectl get sandboxtemplate python-dev -n isola-sandboxes -o yaml
```
