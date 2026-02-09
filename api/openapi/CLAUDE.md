# api/openapi/

Generated OpenAPI 3.1 specifications. **Do not edit these files by hand.**

## Files

- `api-gateway.yaml` — End-user facing REST API spec
- `sandbox-sidecar.yaml` — Internal API spec (api-gateway → sidecar communication)

## Regeneration

```bash
make openapi
```

This runs `cmd/openapi-gen` which registers Huma routes with nil handler dependencies (only type signatures are inspected) and outputs the spec.

CI runs `make check-openapi` to verify these files are in sync with the code.
