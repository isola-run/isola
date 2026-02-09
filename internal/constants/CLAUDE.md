# internal/constants/

Shared constants used across multiple packages.

- `IsolaContainerNameEnv` (`"ISOLA_CONTAINER_NAME"`) — Environment variable injected by the operator into sandbox containers. Used by the sidecar to discover container PIDs via `/proc/<pid>/environ`.
- `SidecarPort` (`10032`) — HTTP port the sandbox-sidecar listens on. Used by both the sidecar binary and the api-gateway filesystem proxy.
