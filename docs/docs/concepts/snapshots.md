---
sidebar_position: 5
title: Filesystem Snapshots
---

# Filesystem Snapshots

Isola supports capturing point-in-time snapshots of sandbox container filesystems via the `RootfsSnapshot` CRD. Snapshots are uploaded to cloud storage.

:::note
Filesystem snapshots require the **gVisor runtime** (`operator.sandboxRuntime.type: gvisor` in the Helm chart). They are not available with the cluster default runtime.
:::

## How It Works

1. Create a `RootfsSnapshot` CR referencing a running sandbox
2. The operator creates an uploader Job for each container to snapshot
3. The uploader tarballs the container's rootfs using gVisor's overlay filesystem
4. The tarball is uploaded to cloud storage (S3, GCS, or Azure Blob Storage)
5. The uploader writes results to the Kubernetes termination log
6. The operator reads the termination log and updates the `RootfsSnapshot` status
7. After `ttlSecondsAfterFinished`, the snapshot CR is automatically deleted

## Creating a Snapshot

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: RootfsSnapshot
metadata:
  name: my-snapshot
  namespace: isola-sandboxes
spec:
  sandboxName: my-sandbox
  activeDeadlineSeconds: 300
  ttlSecondsAfterFinished: 300
```

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `sandboxName` | string | Yes | Name of the target sandbox (must be in the same namespace) |
| `containerNames` | string[] | No | Specific containers to snapshot (all if empty) |
| `activeDeadlineSeconds` | int64 | No | Job timeout in seconds (default: 300) |
| `ttlSecondsAfterFinished` | int32 | No | Auto-delete delay after completion (default: 300) |

## Snapshot Storage

The upload destination is configured in the Helm chart under `operator.sandboxRuntime.gvisor.rootfssnapshot.storage`:

```yaml
storage:
  bucketUrl: "s3://my-bucket?region=us-east-1"
```

Supported cloud storage URLs:

| Provider | Format |
|----------|--------|
| AWS S3 | `s3://bucket-name?region=us-east-1` |
| Google Cloud Storage | `gs://bucket-name` |
| Azure Blob Storage | `azblob://container-name` |
| MinIO / LocalStack | `s3://bucket?endpoint=http://localstack:4566&use_path_style=true` |

### Storage Path Format

Snapshots are stored with the following key pattern:

```
snapshots/<namespace>/<sandbox-name>/rev-<revision>/<container-name>.tar
```

The revision number is auto-incremented by listing existing snapshots in the bucket.

## Snapshot Status

| Condition | Description |
|-----------|-------------|
| `Complete` | Snapshot finished successfully |
| `Failed` | Snapshot encountered an error |

Each container has individual status tracking with its own `snapshotKey` and conditions.

## Security Considerations

Snapshot jobs require elevated privileges:

- **hostPID**: `true` — needed to verify the sandbox process is running
- **hostPath mounts** — for the `runsc` binary and gVisor state directory
- **Root user** — required in the snapshotter init container

If your security policy prohibits these operations, set `operator.sandboxRuntime.gvisor.rootfssnapshot.enabled: false` in the Helm chart. Sandboxes will still run normally; only `RootfsSnapshot` CRs will fail.

## Shutdown Policy Integration

You can configure a sandbox to automatically snapshot its filesystem on shutdown using the `shutdownPolicy` field in the Sandbox CRD:

```yaml
apiVersion: sandbox.isola.run/v1alpha1
kind: Sandbox
metadata:
  name: my-sandbox
  namespace: isola-sandboxes
spec:
  podTemplate:
    spec:
      containers:
        - name: main
          image: python:3.12
  shutdownPolicy:
    strategy: SnapshotRootfs
    activeDeadlineSeconds: 300
```

:::note
The `shutdownPolicy` field is only available through the Sandbox CRD, not through the REST API.
:::

When the sandbox is deleted or times out, the operator creates a `RootfsSnapshot` before cleaning up the pod.
