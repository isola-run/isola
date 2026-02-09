# internal/snapshot/

Shared snapshot types between the operator controller and the uploader binary.

## Types

- `UploadResult` — Contract between uploader and controller. The uploader writes this as JSON to `/dev/termination-log`, and the controller parses it to update `RootfsSnapshotStatus`.

Fields: `SnapshotKey` (object key in bucket), `Revision` (revision number), `BytesWritten`.

This package is the single source of truth — both `cmd/uploader` and `internal/operator/controller` import it.
