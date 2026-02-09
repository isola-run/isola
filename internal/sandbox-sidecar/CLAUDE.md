# internal/sandbox-sidecar/

Sidecar service that runs inside sandbox pods to provide filesystem access.

## Sub-packages

- `handlers/` — HTTP route handlers for health checks and filesystem operations
- `proc/` — `/proc` filesystem abstraction for container PID discovery

## Testing

```bash
make test-sidecar                # Run all sidecar tests
make test-sidecar FOCUS="Write"  # Run focused tests
```

Sidecar tests do not require envtest (no K8s API interaction).
