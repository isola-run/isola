# isola

Umbrella Helm chart for deploying the complete Isola sandbox orchestration stack.

## Overview

This chart installs all Isola components in a single Helm release:

- **isola-operator**: Kubernetes operator for sandbox lifecycle management, including CRDs
- **api-gateway**: REST API for programmatic sandbox orchestration

Using the umbrella chart provides:
- Single `helm install` command for the complete stack
- Unified configuration with shared values (storage, namespace, registry)
- Coordinated upgrades ensuring version compatibility
- GitOps-friendly (works with ArgoCD, Flux)

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
  --set global.storage.bucketUrl="s3://my-bucket?region=us-east-1"
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
  --set isola-operator.runtimeClassName=""
```

## Configuration

### Global Settings

These values are shared across all subcharts:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imageRegistry` | Override registry for all images | `""` |
| `global.imagePullSecrets` | Pull secrets for all pods | `[]` |
| `global.sandboxNamespace` | Namespace for sandbox pods | `isola-sandboxes` |
| `global.storage.bucketUrl` | Storage bucket URL for snapshots | `""` |
| `global.storage.credentials.existingSecret` | Existing secret with credentials | `""` |
| `global.storage.credentials.accessKeyId` | AWS access key (dev only) | `""` |
| `global.storage.credentials.secretAccessKey` | AWS secret key (dev only) | `""` |
| `global.storage.credentials.region` | AWS region | `""` |

### Subchart Configuration

Each subchart can be configured under its namespaced key:

```yaml
# isola-operator specific settings
isola-operator:
  enabled: true
  replicaCount: 1
  runtimeClassName: "gvisor"
  # ... see charts/isola-operator/values.yaml

# api-gateway specific settings
api-gateway:
  enabled: true
  service:
    type: ClusterIP
  # ... see charts/api-gateway/values.yaml
```

### Common Scenarios

#### Production with S3 Storage (Pod Identity)

```yaml
# values-production.yaml
global:
  sandboxNamespace: isola-sandboxes
  storage:
    bucketUrl: "s3://my-bucket?region=us-east-1"

isola-operator:
  runtimeClassName: "gvisor"
  snapshot:
    serviceAccount:
      annotations:
        eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/isola-snapshot"

api-gateway:
  serviceAccount:
    annotations:
      eks.amazonaws.com/role-arn: "arn:aws:iam::123456789:role/isola-api"
  service:
    type: LoadBalancer
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
global:
  storage:
    bucketUrl: "s3://isola-snapshots?endpoint=http://localstack.localstack.svc.cluster.local:4566&use_path_style=true"
    credentials:
      accessKeyId: "test"
      secretAccessKey: "test"
      region: "us-east-1"

isola-operator:
  runtimeClassName: ""  # No gVisor in dev

api-gateway:
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
VERSION=<version>  # Use the version you want to deploy
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
  --set api-gateway.enabled=false
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

## Standalone Charts

For advanced use cases, individual charts can be installed separately:

```bash
# Install operator only
helm install isola-operator oci://ghcr.io/isola-ai/charts/isola-operator \
  --namespace isola-system \
  --create-namespace

# Install API gateway separately (requires operator)
helm install api-gateway oci://ghcr.io/isola-ai/charts/api-gateway \
  --namespace isola-system
```

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │           isola (umbrella)              │
                    │                                         │
                    │  ┌──────────────┐  ┌────────────────┐  │
                    │  │isola-operator│  │  api-gateway   │  │
                    │  │              │  │                │  │
                    │  │ - CRDs       │  │ - REST API     │  │
                    │  │ - Controller │  │ - Auth         │  │
                    │  │ - NetworkPol │  │ - Presigned URL│  │
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
kubectl logs -n isola-system -l app.kubernetes.io/name=isola-operator -f
```

### View API gateway logs
```bash
kubectl logs -n isola-system -l app.kubernetes.io/name=api-gateway -f
```

## Links

- [GitHub Repository](https://github.com/isola-ai/isola-sb)
- [Operator Chart](../isola-operator/README.md)
- [API Gateway Chart](../api-gateway/README.md)
