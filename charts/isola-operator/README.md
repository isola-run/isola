# isola-operator

Kubernetes operator for managing sandboxes.

## Installation

```bash
helm install isola-operator oci://ghcr.io/isola-ai/charts/isola-operator \
  --namespace isola-system \
  --create-namespace
```

## Configuration

See [values.yaml](values.yaml) for all available options.

### Common Configuration

```bash
# With snapshot storage (S3)
helm install isola-operator oci://ghcr.io/isola-ai/charts/isola-operator \
  --namespace isola-system \
  --create-namespace \
  --set snapshot.storage.bucketUrl="s3://my-bucket?region=us-east-1"

# Pin to specific version
helm install isola-operator oci://ghcr.io/isola-ai/charts/isola-operator \
  --namespace isola-system \
  --create-namespace \
  --set image.tag=v0.2.0 \
  --set sidecar.image.tag=v0.2.0 \
  --set snapshot.uploader.image.tag=v0.2.0
```

## Private Registry / Air-Gapped Installation

For environments without internet access or when using a private registry.

### Step 1: Mirror Required Images

Copy these images to your internal registry:

```bash
# isola images (from ghcr.io/isola-ai)
ghcr.io/isola-ai/isola-operator:<version>
ghcr.io/isola-ai/sandbox-sidecar:<version>
ghcr.io/isola-ai/isola-uploader:<version>
ghcr.io/isola-ai/api-gateway:<version>

# Third-party dependency (used by snapshot jobs)
gcr.io/distroless/static:nonroot
```

Example using `skopeo`:
```bash
VERSION=v0.1.0
PRIVATE_REGISTRY=my-registry.example.com/isola

# Mirror isola images
for img in isola-operator sandbox-sidecar isola-uploader api-gateway; do
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
helm install isola-operator ./charts/isola-operator \
  --namespace isola-system \
  --set global.imageRegistry=my-registry.example.com/isola \
  --set global.imagePullSecrets[0].name=regcred
```

This configures:
- All isola images to pull from your registry
- Pull secrets applied to operator deployment, sandbox pods, and snapshot jobs

### Configuration Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `global.imageRegistry` | Override registry for all images | `""` (uses per-image defaults) |
| `global.imagePullSecrets` | Pull secrets for all pods | `[]` |
| `image.registry` | Operator image registry | `ghcr.io/isola-ai` |
| `image.repository` | Operator image repository | `isola-operator` |
| `image.tag` | Operator image tag | Chart appVersion |
| `sidecar.image.registry` | Sidecar image registry | `ghcr.io/isola-ai` |
| `sidecar.image.repository` | Sidecar image repository | `sandbox-sidecar` |
| `sidecar.image.tag` | Sidecar image tag | Chart appVersion |
| `snapshot.uploader.image.registry` | Uploader image registry | `ghcr.io/isola-ai` |
| `snapshot.uploader.image.repository` | Uploader image repository | `isola-uploader` |
| `snapshot.uploader.image.tag` | Uploader image tag | Chart appVersion |

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
