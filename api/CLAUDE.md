# api/

API definitions for the project: Kubernetes CRD types and generated OpenAPI specs.

## Subdirectories

- `v1alpha1/` — Kubernetes CRD Go type definitions (Sandbox, RootfsSnapshot)
- `openapi/` — Generated OpenAPI 3.1 specs (do not edit by hand)

## Workflows

After modifying CRD types in `v1alpha1/*_types.go`:
```bash
make generate manifests
```

After modifying handler types or route registrations:
```bash
make openapi
```
