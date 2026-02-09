# internal/

All business logic. Go's `internal/` visibility rules prevent external imports.

## Package Map

### Service implementations (one per binary)
- `operator/controller/` — Sandbox and RootfsSnapshot reconcilers
- `api-gateway/handlers/` — REST API handlers, types, and CRD conversion
- `sandbox-sidecar/handlers/` — Sidecar HTTP handlers for filesystem operations
- `sandbox-sidecar/proc/` — `/proc` filesystem abstraction for PID discovery

### Shared packages
- `sidecar-api/` — Shared request/response types between api-gateway and sandbox-sidecar
- `snapshot/` — Shared snapshot types between operator and uploader (`UploadResult`)
- `logging/` — Shared slog-based logging configuration
- `constants/` — Shared constants (`ISOLA_CONTAINER_NAME`, `SidecarPort`)
- `env/` — Environment variable parsing utilities

### Test support
- `testutil/utils/` — Test fixtures (functional options), matchers, command helpers
- `testutil/e2e/` — E2E test coordination (Ginkgo)

## Conventions

- Each service's tests live alongside the implementation (`*_test.go` files)
- Test suites use `suite_test.go` for Ginkgo/envtest bootstrap
- Dockerfiles copy all of `internal/` to avoid per-package Dockerfile updates
