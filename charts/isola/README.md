# isola

Kubernetes operator and API for secure sandbox orchestration.

## Installation

```bash
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace
```

## Configuration

See [values.yaml](values.yaml) for all available options.

### Common Configuration

```bash
# With snapshot storage (S3)
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace \
  --set storage.bucketUrl="s3://my-bucket?region=us-east-1"

# Pin to specific version
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace \
  --set operator.image.tag=v0.2.0 \
  --set operator.sidecar.image.tag=v0.2.0 \
  --set operator.snapshot.uploader.image.tag=v0.2.0 \
  --set api.image.tag=v0.2.0

# Disable API (operator-only installation)
helm install isola oci://ghcr.io/isola-ai/charts/isola \
  --namespace isola-system \
  --create-namespace \
  --set api.enabled=false
```

## Private Registry / Air-Gapped Installation

For environments without internet access or when using a private registry.

### Step 1: Mirror Required Images

Copy these images to your internal registry:

```bash
# isola images (from ghcr.io/isola-ai)
ghcr.io/isola-ai/isola-operator:<version>
ghcr.io/isola-ai/isola-sidecar:<version>
ghcr.io/isola-ai/isola-uploader:<version>
ghcr.io/isola-ai/isola-api:<version>

# Third-party dependency (used by snapshot jobs)
gcr.io/distroless/static:nonroot
```

Example using `skopeo`:
```bash
VERSION=v0.1.0
PRIVATE_REGISTRY=my-registry.example.com/isola

# Mirror isola images
for img in isola-operator isola-sidecar isola-uploader isola-api; do
  skopeo copy \
    docker://ghcr.io/isola-ai/${img}:${VERSION} \
    docker://${PRIVATE_REGISTRY}/${img}:${VERSION}
done

# Mirror distroless (used by snapshotter)
skopeo copy \
  docker://gcr.io/distroless/static:nonroot \
  docker://${PRIVATE_REGISTRY}/distroless/static:nonroot
```

### Step 2: Create Image Pull Secret

Create the secret in both namespaces where pods will run:

```bash
# In operator namespace
kubectl create namespace isola-system
kubectl create secret docker-registry regcred \
  --namespace isola-system \
  --docker-server=my-registry.example.com \
  --docker-username=<username> \
  --docker-password=<password>

# In sandbox namespace (where sandbox pods and snapshot jobs run)
kubectl create namespace isola-sandboxes
kubectl create secret docker-registry regcred \
  --namespace isola-sandboxes \
  --docker-server=my-registry.example.com \
  --docker-username=<username> \
  --docker-password=<password>
```

### Step 3: Install with Registry Override

```bash
helm install isola ./charts/isola \
  --namespace isola-system \
  --set global.imageRegistry=my-registry.example.com/isola \
  --set global.imagePullSecrets[0].name=regcred
```

This configures:
- All isola images to pull from your registry
- Pull secrets applied to operator, API, sandbox pods, and snapshot jobs

## Configuration Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imageRegistry` | Override registry for all images | `""` |
| `global.imagePullSecrets` | Pull secrets for all pods | `[]` |
| `sandboxNamespace` | Namespace for sandbox pods | `isola-sandboxes` |
| `runtimeClassName` | RuntimeClass for sandboxes | `gvisor` |
| `priorityClassName` | PriorityClass for sandboxes | `isola-sandbox` |
| `storage.bucketUrl` | Bucket URL for snapshots | `""` |
| `storage.credentials.existingSecret` | Use existing secret | `""` |
| `operator.enabled` | Deploy operator | `true` |
| `operator.image.tag` | Operator image tag | Chart appVersion |
| `operator.sidecar.image.tag` | Sidecar image tag | Chart appVersion |
| `api.enabled` | Deploy API | `true` |
| `api.image.tag` | API image tag | Chart appVersion |
| `api.service.type` | Service type | `ClusterIP` |

## Known Limitations

### Image Pull Secrets in Multiple Namespaces

Kubernetes secrets are namespace-scoped. If using private registries, you must create the `imagePullSecret` in both:
- `isola-system` (operator namespace)
- `isola-sandboxes` (sandbox pod namespace)

The Helm chart cannot create secrets across namespaces.

### SandboxTemplate Image Pull Secrets

If your `SandboxTemplate` references images from a private registry, configure `imagePullSecrets` in the template's `podTemplate.spec`:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: SandboxTemplate
metadata:
  name: my-template
spec:
  podTemplate:
    spec:
      imagePullSecrets:
        - name: my-workload-registry-secret
      containers:
        - name: main
          image: my-registry.example.com/my-app:v1
```

These are separate from `global.imagePullSecrets`, which only applies to isola-managed images (sidecar, uploader).

## CRD Management

CRDs are installed automatically but NOT upgraded by Helm. To upgrade CRDs manually:

```bash
kubectl apply -f https://github.com/isola-ai/isola-sb/releases/download/v0.1.0/crds.yaml
```
