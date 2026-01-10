# Configuration Guide

Complete reference for configuring Isola templates, network policies, and deployment settings.

---

## Table of Contents

- [SandboxTemplate Configuration](#sandboxtemplate-configuration)
- [NetworkTemplate Configuration](#networktemplate-configuration)
- [Gateway Configuration](#gateway-configuration)
- [Operator Configuration](#operator-configuration)
- [Production Best Practices](#production-best-practices)

---

## SandboxTemplate Configuration

### Complete Schema

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: template-name
  namespace: isola-sandboxes
  labels:
    # Custom labels for organization
    language: python
    team: platform
spec:
  # Timeout configuration
  timeoutSeconds: 300           # Required: Auto-termination (1-86400)

  # Shutdown behavior
  shutdownPolicy:
    policy: Delete              # Delete | SnapshotFilesystem

  # Pod specification
  podTemplate:
    metadata:
      labels:
        custom-label: value
      annotations:
        custom-annotation: value
    spec:
      # Container configuration (required)
      containers:
        - name: sandbox         # Must be named "sandbox"
          image: python:3.11-slim
          command: ["sleep", "infinity"]
          args: []
          workingDir: /workspace
          env:
            - name: KEY
              value: "value"
            - name: SECRET
              valueFrom:
                secretKeyRef:
                  name: secret-name
                  key: secret-key
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

      # Volumes
      volumes:
        - name: workspace
          emptyDir:
            sizeLimit: 1Gi

      # Scheduling
      nodeSelector:
        node-type: sandbox
      tolerations:
        - key: "sandbox"
          operator: "Equal"
          value: "true"
          effect: "NoSchedule"
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node-type
                    operator: In
                    values: ["sandbox"]
```

### Common Template Patterns

#### Minimal Python

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-minimal
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 60
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: python:3.11-alpine
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: "50m"
              memory: "64Mi"
            limits:
              cpu: "200m"
              memory: "128Mi"
```

#### Full Development Environment

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: python-dev
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 3600
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
            - name: PYTHONDONTWRITEBYTECODE
              value: "1"
          resources:
            requests:
              cpu: "250m"
              memory: "256Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
          volumeMounts:
            - name: workspace
              mountPath: /workspace
            - name: pip-cache
              mountPath: /root/.cache/pip
      volumes:
        - name: workspace
          emptyDir:
            sizeLimit: 2Gi
        - name: pip-cache
          emptyDir:
            sizeLimit: 500Mi
```

#### Multi-language (Nix)

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: multi-language
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 1800
  podTemplate:
    spec:
      containers:
        - name: sandbox
          image: nixos/nix:latest
          command: ["sleep", "infinity"]
          resources:
            requests:
              cpu: "500m"
              memory: "1Gi"
            limits:
              cpu: "2000m"
              memory: "4Gi"
```

#### With GPU

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: gpu-sandbox
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 7200
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
      nodeSelector:
        nvidia.com/gpu: "true"
      tolerations:
        - key: nvidia.com/gpu
          operator: Exists
          effect: NoSchedule
```

---

## NetworkTemplate Configuration

### Complete Schema

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: template-name
  namespace: isola-sandboxes
spec:
  # Outbound traffic rules
  allowedEgress:
    - "10.0.0.0/8"              # CIDR ranges
    - "172.16.0.0/12"
    - "192.168.0.0/16"

  # Inbound traffic rules (optional)
  allowedIngress:
    - "10.0.0.0/8"

  # DNS servers (max 3)
  dnsServers:
    - "8.8.8.8"
    - "8.8.4.4"
```

### Common Network Patterns

#### Fully Isolated (Default)

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: isola-isolated
  namespace: isola-sandboxes
spec:
  # Empty = deny all
  allowedEgress: []
  allowedIngress: []
```

#### Internet Access

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: internet-access
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "0.0.0.0/0"
  dnsServers:
    - "8.8.8.8"
    - "8.8.4.4"
```

#### Internal Only

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: internal-only
  namespace: isola-sandboxes
spec:
  allowedEgress:
    - "10.0.0.0/8"
    - "172.16.0.0/12"
    - "192.168.0.0/16"
  dnsServers:
    - "10.0.0.53"  # Internal DNS
```

#### Specific Services

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: NetworkTemplate
metadata:
  name: specific-services
  namespace: isola-sandboxes
  annotations:
    description: "Access to GitHub, PyPI, and npm only"
spec:
  allowedEgress:
    # GitHub
    - "140.82.112.0/20"
    - "192.30.252.0/22"
    - "185.199.108.0/22"
    # PyPI (Fastly CDN)
    - "151.101.0.0/16"
    # npm (Cloudflare)
    - "104.16.0.0/12"
  dnsServers:
    - "8.8.8.8"
```

### Blocked CIDRs (Automatic)

The operator automatically blocks these ranges:

| CIDR | Reason |
|------|--------|
| `169.254.0.0/16` | AWS/Cloud metadata (IMDS) |
| `fe80::/10` | IPv6 link-local |

---

## Gateway Configuration

### Helm Values

```yaml
# charts/isola-gw/values.yaml

replicaCount: 2

image:
  repository: isola-gw
  tag: latest
  pullPolicy: IfNotPresent

# API Authentication
auth:
  apiKey: "your-secure-api-key"  # Required

# S3 Storage (for large files)
storage:
  enabled: true
  bucketUrl: "s3://your-bucket-name"
  region: "us-east-1"
  # For AWS IAM roles:
  # useIRSA: true
  # For static credentials:
  accessKeyId: ""
  secretAccessKey: ""

# Kubernetes client config
kubernetes:
  namespace: isola-sandboxes

# Service configuration
service:
  type: ClusterIP
  port: 8080

# Ingress (optional)
ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt
  hosts:
    - host: api.isola.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: isola-api-tls
      hosts:
        - api.isola.example.com

# Resource limits
resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"

# Health probes
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 5
readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `API_KEY` | Authentication key | Required |
| `K8S_NAMESPACE` | Sandbox namespace | `isola-sandboxes` |
| `S3_BUCKET_URL` | S3 bucket for files | None |
| `S3_REGION` | AWS region | `us-east-1` |
| `LOG_LEVEL` | Logging verbosity | `info` |

---

## Operator Configuration

### Helm Values

```yaml
# charts/isola-operator/values.yaml

replicaCount: 1

image:
  repository: isola-operator
  tag: latest

# Agent sidecar image
agentImage:
  repository: isola-agent
  tag: latest

# Runtime class for sandboxes
runtimeClassName: ""  # "gvisor" for enhanced isolation

# Namespace for sandbox pods
sandboxNamespace: isola-sandboxes

# Default templates to create
defaultTemplates:
  enabled: true
  # Creates isola-isolated and isola-egress-only

# Controller configuration
controller:
  # Reconciliation settings
  maxConcurrentReconciles: 10

  # Leader election
  leaderElection:
    enabled: true

# Resource limits
resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "256Mi"
```

### CRD Installation

```bash
# Install CRDs separately (recommended for production)
kubectl apply -f charts/isola-operator/crds/

# Or with Helm
helm install isola-operator ./charts/isola-operator \
  --set installCRDs=true
```

---

## Production Best Practices

### Security

1. **Enable gVisor** for enhanced sandbox isolation:
   ```yaml
   runtimeClassName: gvisor
   ```

2. **Use separate namespaces**:
   ```bash
   kubectl create namespace isola-system
   kubectl create namespace isola-sandboxes
   ```

3. **Rotate API keys** regularly

4. **Enable network policies** by default

5. **Set resource quotas**:
   ```yaml
   apiVersion: v1
   kind: ResourceQuota
   metadata:
     name: sandbox-quota
     namespace: isola-sandboxes
   spec:
     hard:
       pods: "100"
       requests.cpu: "50"
       requests.memory: "100Gi"
   ```

### High Availability

1. **Multiple operator replicas** with leader election:
   ```yaml
   replicaCount: 3
   controller:
     leaderElection:
       enabled: true
   ```

2. **Gateway replicas** behind load balancer:
   ```yaml
   replicaCount: 3
   service:
     type: LoadBalancer
   ```

3. **Pod disruption budgets**:
   ```yaml
   apiVersion: policy/v1
   kind: PodDisruptionBudget
   metadata:
     name: isola-operator
   spec:
     minAvailable: 1
     selector:
       matchLabels:
         app: isola-operator
   ```

### Monitoring

1. **Enable metrics**:
   ```yaml
   metrics:
     enabled: true
     port: 8080
   ```

2. **Configure Prometheus**:
   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: isola-operator
   spec:
     selector:
       matchLabels:
         app: isola-operator
     endpoints:
       - port: metrics
   ```

3. **Set up alerts**:
   ```yaml
   - alert: SandboxCreationFailing
     expr: increase(sandbox_creation_failures_total[5m]) > 5
     for: 5m
     labels:
       severity: warning
   ```

### Resource Management

1. **Set default limits** in namespace:
   ```yaml
   apiVersion: v1
   kind: LimitRange
   metadata:
     name: sandbox-limits
     namespace: isola-sandboxes
   spec:
     limits:
       - default:
           cpu: "500m"
           memory: "512Mi"
         defaultRequest:
           cpu: "100m"
           memory: "128Mi"
         type: Container
   ```

2. **Use node pools** for sandboxes:
   ```yaml
   nodeSelector:
     node-pool: sandbox
   tolerations:
     - key: "sandbox-only"
       operator: "Equal"
       value: "true"
       effect: "NoSchedule"
   ```

3. **Configure cluster autoscaler**:
   ```yaml
   # For sandbox node pool
   minSize: 1
   maxSize: 100
   ```

---

## Example: Complete Production Setup

```bash
# 1. Create namespaces
kubectl create namespace isola-system
kubectl create namespace isola-sandboxes

# 2. Install CRDs
kubectl apply -f charts/isola-operator/crds/

# 3. Install operator
helm install isola-operator ./charts/isola-operator \
  --namespace isola-system \
  --set runtimeClassName=gvisor \
  --set replicaCount=2

# 4. Install gateway
helm install isola-gw ./charts/isola-gw \
  --namespace isola-system \
  --set auth.apiKey="$(openssl rand -hex 32)" \
  --set storage.enabled=true \
  --set storage.bucketUrl="s3://my-isola-bucket" \
  --set replicaCount=3

# 5. Create default templates
kubectl apply -f - <<EOF
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: default
  namespace: isola-sandboxes
spec:
  timeoutSeconds: 300
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
EOF

# 6. Verify installation
kubectl get pods -n isola-system
kubectl get sandboxtemplates -n isola-sandboxes
```
