---
sidebar_position: 2
title: Configuration
---

# Configuration

This page covers configuration options and runtime modes for Isola.

## Runtime Types

Isola supports two runtime types for sandbox pods:

### gVisor (Default)

```yaml
operator:
  sandboxRuntime:
    type: gvisor
    gvisor:
      runtimeClassName: gvisor
```

- Uses gVisor (`runsc`) for kernel-level sandbox isolation
- Enables filesystem snapshot capability
- Requires a `RuntimeClass` named `gvisor` (or custom name) in the cluster
- Recommended for production use

### Cluster Default

```yaml
operator:
  sandboxRuntime:
    type: clusterDefault
```

- Uses the cluster's default container runtime (typically `runc`)
- Filesystem snapshots are **not available**
- Useful for testing or environments where gVisor cannot be installed

## Network Configuration

### Static NetworkPolicies

The Helm chart deploys static NetworkPolicies that apply to all sandboxes via label selectors:

| Policy | Matches |
|--------|---------|
| Default deny egress | All sandbox pods |
| Allow ingress from gateway | All sandbox pods |
| Allow internet egress | Pods with `isola.run/allow-internet-egress=true` |
| Allow cluster DNS | Pods with `isola.run/allow-cluster-dns=true` |

### Per-Sandbox NetworkPolicies

The operator creates additional NetworkPolicies for sandboxes with custom `allowedEgressCIDRs` or `nameservers` when `allowInternetEgress` is not enabled. These are cleaned up by the finalizer when the sandbox is deleted.

## Operator Flags

The operator binary accepts the following flags, which are configured through the Helm chart:

| Flag | Helm Value | Description |
|------|-----------|-------------|
| `--sidecar-image` | `operator.sidecar.image.*` | Sidecar image injected into sandbox pods |
| `--runtime-class` | `operator.sandboxRuntime.gvisor.runtimeClassName` | RuntimeClass for sandbox pods |
| `--priority-class` | `operator.sandboxPriorityClassName` | PriorityClass for sandbox pods |
| `--sandbox-namespace` | `sandboxNamespace` | Namespace for sandbox pods |

## API Gateway Configuration

The API gateway connects to the Kubernetes API and proxies requests to sandbox sidecars.

| Environment Variable | Description |
|---------------------|-------------|
| `ISOLA_SANDBOX_NAMESPACE` | Namespace where sandboxes are created (required) |
| `ISOLA_LOG_LEVEL` | Logging level: `debug`, `info`, `warn`, `error` (default: `info`) |
| `ISOLA_DEV_MODE` | Set to any non-empty value to enable human-readable logging |
| `ISOLA_HTTP_PORT` | HTTP server port (default: `8080`) |

The gateway listens on port `8080` and provides health endpoints at `/health`, `/healthz`, `/ready`, and `/readyz`.

## CRD Management

CRDs are installed via the `charts/isola/crds/` directory. They are generated from Go type definitions:

```bash
# After modifying api/v1alpha1/*_types.go
make generate    # Regenerate DeepCopy methods
make manifests   # Regenerate CRD YAML and RBAC
```

:::warning
CRDs are installed separately from the Helm release and are not automatically upgraded. When upgrading Isola, apply CRD updates first:

```bash
kubectl apply -f charts/isola/crds/
```
:::

## Generated Files

The following files are generated and should not be edited manually:

| Source | Generated Files | Command |
|--------|----------------|---------|
| `api/v1alpha1/*_types.go` | `charts/isola/crds/`, `charts/isola/generated/role.yaml` | `make generate manifests` |
| Handler types and routes | `api/openapi/api-gateway.yaml`, `api/openapi/sandbox-sidecar.yaml` | `make openapi` |
