# internal/operator/controller/snapshot/

Snapshot support utilities for the RootfsSnapshot reconciler.

## Key Functions

- `CheckRootfsSnapshotSupport()` — Checks if a pod's RuntimeClass supports rootfs snapshotting by looking up the RuntimeClass object and verifying it uses a gVisor/runsc handler. Returns false for pods without a RuntimeClassName or with non-gVisor runtimes.

The caller should verify pod readiness before calling this function.
