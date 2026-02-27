---
sidebar_position: 1
title: Helm Chart
---

# Helm Chart

Isola is deployed via a Helm chart that installs the operator, API gateway, and supporting resources.

## Prerequisites

- Kubernetes v1.25+
- Helm v3
- gVisor installed on cluster nodes (for default runtime)

## Installation

```bash
helm install isola ./charts/isola \
  --namespace isola-system \
  --create-namespace
```

## Chart Overview

The chart deploys the following resources:

| Resource | Namespace | Description |
|----------|-----------|-------------|
| Operator Deployment | `isola-system` | Sandbox and RootfsSnapshot controller |
| API Gateway Deployment | `isola-system` | REST API server |
| CRDs | Cluster-scoped | Sandbox, RootfsSnapshot |
| ClusterRole / ClusterRoleBinding | Cluster-scoped | Operator RBAC |
| NetworkPolicies | `isola-sandboxes` | Default sandbox isolation rules |
| Sandbox Namespace | `isola-sandboxes` | Where sandbox pods are created |

## Key Values

### Global

```yaml
global:
  imageRegistry: ""          # Override registry for all images
  imagePullSecrets: []       # Pull secrets for private registries

sandboxNamespace: isola-sandboxes    # Where sandbox pods are created
createSandboxNamespace: true         # Create the namespace automatically
```

### Operator

```yaml
operator:
  image:
    registry: ghcr.io/isola-ai
    repository: isola-operator
    tag: ""                  # Defaults to Chart.appVersion
    pullPolicy: IfNotPresent

  replicaCount: 1

  resources:
    requests:
      cpu: 50m
      memory: 128Mi

  logging:
    level: info              # info, debug, error
    devMode: false           # Human-readable vs JSON logging

  sidecar:
    image:
      registry: ghcr.io/isola-ai
      repository: sandbox-sidecar
      tag: ""                # Defaults to Chart.appVersion
```

### Sandbox Runtime

```yaml
operator:
  sandboxRuntime:
    type: gvisor             # gvisor or clusterDefault

    gvisor:
      runtimeClassName: gvisor

      rootfssnapshot:
        enabled: true
        runsc:
          binaryPath: /usr/local/bin/runsc
          rootDir: /run/containerd/runsc/k8s.io
        storage:
          bucketUrl: ""      # Required for snapshotting
          credentials:
            existingSecret: ""
            accessKeyId: ""
            secretAccessKey: ""
            region: ""
        uploader:
          image:
            registry: ghcr.io/isola-ai
            repository: isola-uploader
            tag: ""

    clusterDefault: {}       # No special configuration needed
```

### API Gateway

```yaml
apiGateway:
  enabled: true              # Can be disabled if not needed

  image:
    registry: ghcr.io/isola-ai
    repository: api-gateway
    tag: ""
    pullPolicy: IfNotPresent

  replicaCount: 1

  resources:
    requests:
      cpu: 10m
      memory: 64Mi

  service:
    type: ClusterIP
    nodePort: null
    annotations: {}

  logging:
    level: info
    devMode: false
```

### Common Pod Settings

Both the operator and API gateway support:

```yaml
nodeSelector: {}
tolerations: []
affinity: {}
topologySpreadConstraints: []
podAnnotations: {}

serviceAccount:
  create: true
  name: ""
  annotations: {}
```

## Snapshot Storage Configuration

For filesystem snapshots, configure the storage backend:

### AWS S3

```yaml
operator:
  sandboxRuntime:
    gvisor:
      rootfssnapshot:
        storage:
          bucketUrl: "s3://my-bucket?region=us-east-1"
```

### Google Cloud Storage

```yaml
operator:
  sandboxRuntime:
    gvisor:
      rootfssnapshot:
        storage:
          bucketUrl: "gs://my-bucket"
```

### Azure Blob Storage

```yaml
operator:
  sandboxRuntime:
    gvisor:
      rootfssnapshot:
        storage:
          bucketUrl: "azblob://my-container"
```

### MinIO / LocalStack (Development)

```yaml
operator:
  sandboxRuntime:
    gvisor:
      rootfssnapshot:
        storage:
          bucketUrl: "s3://my-bucket?endpoint=http://localstack:4566&use_path_style=true"
          credentials:
            accessKeyId: "test"
            secretAccessKey: "test"
```

## Credential Management

Three modes for snapshot storage credentials:

1. **Pod/Workload Identity** (recommended): Leave all credential fields empty. Configure IRSA (AWS), Workload Identity (GCP), or Pod Identity (Azure) on the snapshot service account.

2. **Existing Secret**: Set `credentials.existingSecret` to reference a pre-created Kubernetes secret.

3. **Chart-managed** (dev only): Set `credentials.accessKeyId` and `credentials.secretAccessKey` directly. Not recommended for production.
