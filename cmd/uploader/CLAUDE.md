# cmd/uploader/

Snapshot uploader job entry point. Runs as a container in RootfsSnapshot Jobs to upload rootfs tarballs to cloud object storage.

## Environment Variables (all required)

- `ISOLA_BUCKET_URL` — Bucket URL (e.g., `s3://bucket?region=us-east-1`)
- `SNAPSHOT_FILE` — Path to local tarball file to upload
- `SNAPSHOT_NAMESPACE` — Sandbox namespace
- `SNAPSHOT_SANDBOX_NAME` — Sandbox name
- `SNAPSHOT_CONTAINER_NAME` — Container name being snapshotted
- `ISOLA_LOG_LEVEL` — Log level (default: `info`)

## Key Behavior

- Determines revision number by listing existing snapshots in the bucket (prefix-based)
- Upload key format: `snapshots/<namespace>/<sandbox>/rev-<revision>/<container>.tar`
- Writes `snapshot.UploadResult` JSON to `/dev/termination-log` for the operator controller to read
- Supports S3, GCS, and Azure Blob Storage via `gocloud.dev/blob` drivers
- Always uses JSON logging (no dev mode)

## Required Bucket Permissions

- **S3:** `s3:ListBucket`, `s3:PutObject`
- **GCS:** `storage.objects.list`, `storage.objects.create`
- **Azure:** Storage Blob Data Contributor role or equivalent
