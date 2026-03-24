// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

export const SandboxStatus = {
  Creating: "creating",
  Running: "running",
  ShuttingDown: "shuttingDown",
  Failed: "failed",
  Stopped: "stopped",
  Unknown: "unknown",
} as const;

export type SandboxStatus = (typeof SandboxStatus)[keyof typeof SandboxStatus];

// ---------------------------------------------------------------------------
// Network & Resources
// ---------------------------------------------------------------------------

export interface NetworkSpec {
  allowInternetEgress?: boolean;
  allowClusterDNS?: boolean;
  allowedEgressCIDRs?: string[];
  nameservers?: string[];
}

export interface ResourceList {
  cpu?: string;
  memory?: string;
  ephemeralStorage?: string;
}

export interface ResourcesSpec {
  limits?: ResourceList;
  requests?: ResourceList;
}

// ---------------------------------------------------------------------------
// Container
// ---------------------------------------------------------------------------

/** Container specification sent in create requests. */
export interface ContainerSpec {
  image: string;
  command?: string[];
  env?: Record<string, string>;
  resources?: ResourcesSpec;
}

/** Container information returned in responses (env intentionally omitted). */
export interface ContainerInfo {
  image: string;
  command?: string[];
  resources?: ResourcesSpec;
}

// ---------------------------------------------------------------------------
// Pod Template
// ---------------------------------------------------------------------------

export interface PodTemplate {
  container: ContainerSpec;
}

export interface PodTemplateInfo {
  container: ContainerInfo;
}

// ---------------------------------------------------------------------------
// Snapshot
// ---------------------------------------------------------------------------

export interface RootfsSnapshotSource {
  snapshotName: string;
  containerName?: string;
}

// ---------------------------------------------------------------------------
// Sandbox (request / response)
// ---------------------------------------------------------------------------

/** @internal Payload sent to POST /v1/sandboxes. */
export interface CreateSandboxPayload {
  podTemplate: PodTemplate;
  activeDeadlineSeconds?: number;
  network?: NetworkSpec;
  rootfsSnapshotSources?: RootfsSnapshotSource[];
}

export interface SandboxSummary {
  id: string;
  status: SandboxStatus;
  creationTimestamp: string;
}

export interface SandboxData {
  id: string;
  podTemplate: PodTemplateInfo;
  status: SandboxStatus;
  creationTimestamp: string;
  activeDeadlineSeconds?: number;
  network?: NetworkSpec;
  rootfsSnapshotSources?: RootfsSnapshotSource[];
}

/** @internal Wrapper for the list endpoint response. */
export interface ListSandboxesResponse {
  sandboxes?: SandboxSummary[];
}

// ---------------------------------------------------------------------------
// Command (request / response)
// ---------------------------------------------------------------------------

/** @internal Payload sent to POST /v1/sandboxes/{id}/commands. */
export interface CreateCommandPayload {
  args: readonly string[];
  env?: Record<string, string>;
  cwd?: string;
  timeout?: number;
}

/** @internal Response from POST /v1/sandboxes/{id}/commands (202). */
export interface CreateCommandResponse {
  commandId: string;
}

/** @internal Response from GET .../commands/{id}/status. */
export interface CommandStatusResponse {
  exitCode: number | null;
}

/** Result returned by {@link Commands.run}. */
export interface CommandResult {
  readonly commandId: string;
  readonly stdout: string;
  readonly stderr: string;
  readonly exitCode: number;
}

// ---------------------------------------------------------------------------
// Filesystem
// ---------------------------------------------------------------------------

export interface FileWriteResult {
  absolutePath: string;
  bytesWritten: number;
}
