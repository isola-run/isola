# Isola Helm Chart

Helm chart for deploying Isola, a secure sandbox orchestration platform for Kubernetes.

## Overview

This chart installs the complete Isola stack:

- **Operator**: Kubernetes operator for sandbox lifecycle management, CRDs, and network policies
- **API Gateway** (optional): REST API for programmatic sandbox orchestration

## Quick Start

```bash
# Install with default configuration
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace

# Install with snapshot storage enabled
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace \
  --set operator.snapshot.storage.bucketUrl="s3://my-bucket?region=us-east-1"
```

### Installing from Source

```bash
helm install isola charts/isola \
  --namespace isola-system \
  --create-namespace
```

## Prerequisites

- Kubernetes 1.25+
- Helm 3.10+
- (Recommended) gVisor runtime class for sandbox isolation

### gVisor Setup

For production use, configure gVisor on your cluster nodes:

```bash
# GKE Autopilot/Standard: Enable gVisor node pool
# See: https://cloud.google.com/kubernetes-engine/docs/how-to/sandbox-pods

# Self-managed clusters: Install gVisor and create RuntimeClass
kubectl apply -f - <<EOF
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
EOF
```

To run without gVisor (development only):
```bash
helm install isola ./charts/isola \
  --set operator.runtimeClassName=""
```

## Configuration

### Global Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imageRegistry` | Override registry for all images | `""` |
| `global.imagePullSecrets` | Pull secrets for all pods | `[]` |
| `sandboxNamespace` | Namespace where sandbox pods are created | `isola-sandboxes` |

### Operator Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `operator.image.registry` | Operator image registry | `ghcr.io/isola-ai` |
| `operator.image.repository` | Operator image repository | `isola-operator` |
| `operator.image.tag` | Operator image tag | `""` (uses Chart.appVersion) |
| `operator.replicaCount` | Number of operator replicas | `1` |
| `operator.runtimeClassName` | RuntimeClass for sandbox pods | `gvisor` |
| `operator.priorityClassName` | PriorityClass for sandbox pods | `isola-sandbox` |
| `operator.sidecar.image.*` | Sidecar image injected into sandboxes | See values.yaml |
| `operator.snapshot.storage.bucketUrl` | Bucket URL for snapshots | `""` |
| `operator.snapshot.storage.credentials.*` | Storage credentials | See values.yaml |
| `operator.sandboxPdb.enabled` | Enable PodDisruptionBudget for sandboxes | `false` |
| `operator.sandboxPdb.maxUnavailable` | Max unavailable sandboxes during disruption | `0` |

### API Gateway Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `apiGateway.enabled` | Enable the API gateway | `true` |
| `apiGateway.image.registry` | API gateway image registry | `ghcr.io/isola-ai` |
| `apiGateway.image.repository` | API gateway image repository | `api-gateway` |
| `apiGateway.image.tag` | API gateway image tag | `""` (uses Chart.appVersion) |
| `apiGateway.replicaCount` | Number of API gateway replicas | `1` |
| `apiGateway.service.type` | Service type | `ClusterIP` |
| `apiGateway.service.nodePort` | NodePort (when type is NodePort) | `null` |
| `apiGateway.logging.level` | Log level | `info` |
| `apiGateway.logging.devMode` | Enable development mode logging | `false` |

### Common Scenarios

#### Production with S3 Storage (Pod Identity)

```yaml
# values-production.yaml
sandboxNamespace: isola-sandboxes

operator:
  runtimeClassName: "gvisor"
  snapshot:
    storage:
      bucketUrl: "s3://my-bucket?region=us-east-1"
    serviceAccount:
      annotations:
        eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/isola-snapshot"

apiGateway:
  service:
    type: LoadBalancer
  serviceAccount:
    annotations:
      eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/isola-api"
```

```bash
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace \
  -f values-production.yaml
```

#### Development with LocalStack

```yaml
# values-dev.yaml
operator:
  runtimeClassName: ""  # No gVisor in dev
  snapshot:
    storage:
      bucketUrl: "s3://isola-snapshots?endpoint=http://localstack.localstack.svc.cluster.local:4566&use_path_style=true"
      credentials:
        accessKeyId: "test"
        secretAccessKey: "test"
        region: "us-east-1"

apiGateway:
  service:
    type: NodePort
    nodePort: 30080
  logging:
    level: debug
    devMode: true
```

#### Private Registry / Air-Gapped

```bash
# Step 1: Mirror images to your registry
VERSION=<version>
REGISTRY=my-registry.example.com/isola

for img in isola-operator sandbox-sidecar isola-uploader api-gateway; do
  skopeo copy \
    docker://ghcr.io/isola-ai/${img}:${VERSION} \
    docker://${REGISTRY}/${img}:${VERSION}
done

# Step 2: Create pull secrets
kubectl create namespace isola-system
kubectl create namespace isola-sandboxes

for ns in isola-system isola-sandboxes; do
  kubectl create secret docker-registry regcred \
    --namespace $ns \
    --docker-server=my-registry.example.com \
    --docker-username=<user> \
    --docker-password=<pass>
done

# Step 3: Install with registry override
helm install isola ./charts/isola \
  --namespace isola-system \
  --set global.imageRegistry=my-registry.example.com/isola \
  --set global.imagePullSecrets[0].name=regcred
```

#### Disable API Gateway (Operator Only)

```bash
helm install isola ./charts/isola \
  --namespace isola-system \
  --create-namespace \
  --set apiGateway.enabled=false
```

## Upgrading

```bash
# Standard upgrade
helm upgrade isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --reuse-values

# Upgrade with new values
helm upgrade isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  -f values-production.yaml
```

## Uninstalling

```bash
helm uninstall isola --namespace isola-system
```

Note: CRDs are not automatically deleted. To remove them:
```bash
kubectl delete crd sandboxes.sandbox.isola.run
kubectl delete crd sandboxtemplates.sandbox.isola.run
kubectl delete crd rootfssnapshots.sandbox.isola.run
```

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              isola-system               │
                    │                                         │
                    │  ┌──────────────┐  ┌────────────────┐  │
                    │  │   Operator   │  │  API Gateway   │  │
                    │  │              │  │   (optional)   │  │
                    │  │ - CRDs       │  │                │  │
                    │  │ - Controller │  │ - REST API     │  │
                    │  │ - NetworkPol │  │ - Health check │  │
                    │  └──────────────┘  └────────────────┘  │
                    └─────────────────────────────────────────┘
                                      │
                    ┌─────────────────┴─────────────────┐
                    │        isola-sandboxes namespace   │
                    │                                    │
                    │  ┌─────────┐ ┌─────────┐          │
                    │  │ Sandbox │ │ Sandbox │  ...     │
                    │  │   Pod   │ │   Pod   │          │
                    │  └─────────┘ └─────────┘          │
                    └────────────────────────────────────┘
```

## Troubleshooting

### Check component status
```bash
kubectl get pods -n isola-system
kubectl get sandboxes -A
kubectl get sandboxtemplates -A
```

### View operator logs
```bash
kubectl logs -n isola-system -l app.kubernetes.io/component=operator -f
```

### View API gateway logs
```bash
kubectl logs -n isola-system -l app.kubernetes.io/component=api-gateway -f
```

## Links

- [GitHub Repository](https://github.com/isola-ai/isola-sb)
