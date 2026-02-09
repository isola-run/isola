# cmd/

Executable entry points. Each subdirectory produces one binary. Entry points are thin — business logic lives in `internal/`.

## Binaries

| Directory | Binary | Description |
|-----------|--------|-------------|
| `operator/` | `operator` | Kubernetes operator (Sandbox + RootfsSnapshot reconcilers) |
| `api-gateway/` | `api-gateway` | REST API gateway for external clients |
| `sandbox-sidecar/` | `sandbox-sidecar` | Sidecar injected into sandbox pods by operator |
| `uploader/` | `uploader` | Snapshot uploader job (tarballs rootfs → S3/GCS/Azure) |
| `openapi-gen/` | `openapi-gen` | CLI tool to generate OpenAPI specs from Huma types |

## Build

```bash
make build  # Builds all binaries to bin/
```

## Dockerfiles

Each binary directory contains its own `Dockerfile`. All Dockerfiles copy the entire `internal/` directory rather than individual packages to avoid needing updates when adding new internal packages.
