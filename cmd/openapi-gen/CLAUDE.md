# cmd/openapi-gen/

CLI tool that generates OpenAPI 3.1 specs from Huma route registrations without starting servers.

## Usage

```bash
go run ./cmd/openapi-gen -service api-gateway > api/openapi/api-gateway.yaml
go run ./cmd/openapi-gen -service sandbox-sidecar > api/openapi/sandbox-sidecar.yaml
```

Or via Make:
```bash
make openapi
```

## How It Works

Registers routes with nil handler dependencies (handlers are never called — only their type signatures are inspected by Huma to generate the OpenAPI spec). Outputs YAML or JSON to stdout.

## Flags

- `-service` (required) — `api-gateway` or `sandbox-sidecar`
- `-format` — `yaml` (default) or `json`
