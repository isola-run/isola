package constants

// IsolaContainerNameEnv is the environment variable used to mark containers
// with their name for discovery by the sidecar via /proc/<pid>/environ.
const IsolaContainerNameEnv = "ISOLA_CONTAINER_NAME"

// SidecarPort is the HTTP port the sandbox-sidecar listens on.
const SidecarPort = 10032
