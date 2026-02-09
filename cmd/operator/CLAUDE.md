# cmd/operator/

Kubernetes operator entry point. Registers and starts the `SandboxReconciler` and `RootfsSnapshotReconciler` with controller-runtime.

## Configuration

Configuration is via CLI flags with environment variable fallbacks:

- `--sidecar-image` / `ISOLA_SIDECAR_IMAGE` (required) — Container image for sandbox-sidecar
- `--runtime-class` — RuntimeClassName for sandbox pods (default: `gvisor`)
- `--priority-class` — PriorityClassName for sandbox pods
- `--rootfssnapshot-enabled` / `ISOLA_ROOTFSSNAPSHOT_ENABLED` — Enable snapshot capability
- `--rootfssnapshot-bucket-url` / `ISOLA_ROOTFSSNAPSHOT_BUCKET_URL` — Bucket URL for snapshots
- `--rootfssnapshot-uploader-image` / `ISOLA_UPLOADER_IMAGE` — Uploader container image
- `--rootfssnapshot-credential-secret` — Secret name for bucket credentials
- `--gvisor-runsc-path` / `ISOLA_GVISOR_RUNSC_PATH` — Path to runsc binary on nodes
- `--gvisor-runsc-root` / `ISOLA_GVISOR_RUNSC_ROOT` — Root directory for runsc state
- `--image-pull-secrets` / `ISOLA_IMAGE_PULL_SECRETS` — Comma-separated imagePullSecret names
- `--runtime-type` / `ISOLA_RUNTIME_TYPE` — Runtime type: `gvisor` or `clusterDefault`

## Key Behavior

- Registers CRD scheme (`sandbox.isola.run/v1alpha1`)
- Creates two reconcilers: `SandboxReconciler` (pod lifecycle) and `RootfsSnapshotReconciler` (snapshot jobs)
- Uses `RealClock` for production; tests substitute `FakeClock`
- Leader election ID: `3e5ad6c4.isola.run`
- HTTP/2 disabled by default (CVE mitigation)
