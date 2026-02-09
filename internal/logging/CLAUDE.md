# internal/logging/

Shared slog-based logging configuration used by all services.

## Usage

```go
logger := logging.New(logging.Config{
    Level:   "info",    // debug, info, warn, error
    DevMode: true,      // true = text output, false = JSON output
})
```

- Output goes to stderr
- JSON format by default (production), text format in dev mode
- Used by operator (bridged to logr via `logr.FromSlogHandler`), api-gateway, sandbox-sidecar, and uploader
