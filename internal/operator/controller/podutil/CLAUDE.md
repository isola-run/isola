# internal/operator/controller/podutil/

Pod utility functions used by the operator controllers.

## Key Functions

- `IsPodReady()` — Checks if a pod is in Running phase with Ready condition true
- `IsJobFinished()` — Checks if a batch Job has completed or failed
- Pod naming and DNS-safe label helpers
- Sidecar container injection utilities
