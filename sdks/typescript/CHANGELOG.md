# @isola-run/sdk

## 0.5.0

Initial public release of the Isola TypeScript SDK. Feature parity with the
Python SDK is the goal; TS-idiomatic extras (`await using`, `AbortSignal`,
per-call timeouts, `ReadableStream` uploads) are layered on top.

### Highlights

- `Isola` async client with `await using` automatic resource cleanup
- Sandboxes: create, list, get, delete; single-container shorthand
  (`image`/`cpu`/`memory`) and multi-container (`containers: [...]`)
- Commands: `run` (blocking, optional `waitTimeoutMs`), `spawn` (non-blocking
  with streaming `stdout`/`stderr`, `Last-Event-ID` resume on reconnect),
  `kill`, `wait({ timeoutMs })`, `writeStdin`/`closeStdin`
- Filesystem: `read`/`write` accepting `string`, `Uint8Array`, `Blob`, or
  `ReadableStream` (streaming bodies are non-replayable)
- Rootfs snapshots: `create`, `get`, `restore` via `rootfsSnapshotName`,
  automatic snapshot on termination via `terminationPolicy`
- Network policy: internet egress, CIDR allowlists, custom nameservers, IPv6
- Per-call cancellation via `AbortSignal`; per-attempt `requestTimeoutMs`
- Automatic retry on transient errors (HTTP 502/503/504, transport failures):
  up to 6 attempts with a fixed 1 s delay between attempts
- Typed error hierarchy: `IsolaError` -> `APIError` (`BadRequestError`,
  `NotFoundError`, `ConflictError`, `ValidationError`, `InternalError`,
  `BadGatewayError`), `IsolaTimeoutError`, `APIConnectionError`
- Runtime support: Node 22+, Bun, Deno are exercised in CI; any runtime
  providing the WHATWG Fetch API should work. No browser build.
- Dual ESM + CJS publish with `attw`-verified types, npm provenance, and OIDC
  trusted publishing.
