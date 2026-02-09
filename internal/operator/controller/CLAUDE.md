# internal/operator/controller/

Reconciler implementations for the Sandbox and RootfsSnapshot CRDs.

## Reconcilers

### SandboxReconciler (`sandbox_controller.go`)
Manages Sandbox lifecycle: pod creation, status conditions, network policy, timeout, finalizer cleanup, and shutdown policy (delete or snapshot-then-delete).

Key fields:
- `Client`, `Scheme` — controller-runtime dependencies
- `SandboxSidecarImage` — injected into sandbox pods
- `RuntimeClassName`, `PriorityClassName` — pod spec overrides
- `ImagePullSecrets` — for private registries
- `Clock` — `Clock` interface for deterministic time in tests

### RootfsSnapshotReconciler (`rootfssnapshot_controller.go`)
Manages RootfsSnapshot lifecycle: validates runtime support, creates uploader Jobs (one per container), reads termination logs for upload results, handles TTL-based auto-deletion.

Key fields:
- `BucketURL`, `CredentialSecretName`, `UploaderImage` — snapshot storage config
- `Enabled` — gates snapshot capability
- `GvisorRunscPath`, `GvisorRunscRoot` — gVisor-specific paths
- `Clock` — same `Clock` interface for time control

## Clock Abstraction (`clock.go`)

`Clock` interface with `Now()`, `Since()`, `Until()` methods. `RealClock` for production, `FakeClock` for tests. `FakeClock` supports `Advance(d)` and `Set(t)` for deterministic timeout and TTL testing.

## Testing (`suite_test.go`)

- Uses Ginkgo/Gomega with envtest
- **Two-client pattern**: `k8sClient` (direct, no cache delay) for test writes/assertions, `k8sCache` (cached via manager) for reconciler field index queries
- CRDs loaded from `charts/isola/crds/`
- Test namespace: `test-sandbox`
- `FakeRecorder` captures K8s events for assertions
- Helper functions: `newTestReconciler()`, `newTestReconcilerWithRecorder()`, `newTestReconcilerWithRuntimeClass()`

## Sub-packages

- `network/` — Custom per-sandbox NetworkPolicy builder
- `network/cidr/` — CIDR validation, blocked range definitions, exception computation
- `podutil/` — Pod readiness checks, DNS-safe naming, label helpers
- `snapshot/` — Runtime class support checking for rootfs snapshots

## Condition Constants

Status conditions and reasons are defined as constants at the top of `sandbox_controller.go`. The mapping from condition reasons to user-facing status strings (`creating`, `running`, `shuttingDown`, `failed`, `stopped`) lives in `internal/api-gateway/handlers/convert.go:conditionsToStatus()`.
