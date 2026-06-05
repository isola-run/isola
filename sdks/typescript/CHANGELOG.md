# @isola-run/sdk

## 0.6.0-rc.0

Initial public release of the Isola TypeScript SDK. Feature parity with the
Python SDK, with TS-idiomatic ergonomics (`AbortSignal` cancellation, per-call
timeouts, `ReadableStream` uploads).

### Highlights

- `Isola` async client
- Sandboxes: create, list, get, and delete, with single-container shorthand and
  multi-container support
- Commands: `run` (blocking) and `spawn` (non-blocking) with streaming
  `stdout`/`stderr`, `kill`, `wait`, and stdin control
- Filesystem: `read` and `write`
- Rootfs snapshots: create, get, restore, and automatic snapshot on termination
- Network policy: internet egress, CIDR allowlists, custom nameservers, and IPv6
- Per-call cancellation via `AbortSignal` and automatic retry on transient errors
- Typed error hierarchy
- Runtime support: Node 22+, Bun, and Deno
