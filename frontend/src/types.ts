// API types matching the OpenAPI spec

export interface SandboxSummary {
  id: string;
  status: string;
  creationTimestamp: string;
}

export interface ListSandboxesResponse {
  sandboxes: SandboxSummary[] | null;
}

export interface ResourceList {
  cpu?: string;
  memory?: string;
  ephemeralStorage?: string;
}

export interface ResourcesSpec {
  requests?: ResourceList;
  limits?: ResourceList;
}

export interface ContainerSpec {
  image: string;
  command?: string[] | null;
  env?: Record<string, string>;
  resources?: ResourcesSpec;
}

export interface ContainerInfo {
  image: string;
  command?: string[] | null;
  resources?: ResourcesSpec;
}

export interface PodTemplate {
  container: ContainerSpec;
}

export interface PodTemplateInfo {
  container: ContainerInfo;
}

export interface NetworkSpec {
  allowClusterDNS?: boolean;
  allowInternetEgress?: boolean;
  allowedEgressCIDRs?: string[] | null;
  nameservers?: string[] | null;
}

export interface RootfsSnapshotSource {
  snapshotName: string;
  containerName?: string;
}

export interface CreateSandboxRequest {
  podTemplate: PodTemplate;
  timeoutSeconds?: number;
  startupTimeoutSeconds?: number;
  network?: NetworkSpec;
  rootfsSnapshotSources?: RootfsSnapshotSource[] | null;
}

export type SandboxStatus =
  | "creating"
  | "running"
  | "shuttingDown"
  | "failed"
  | "stopped"
  | "unknown";

export interface SandboxResponse {
  id: string;
  podTemplate: PodTemplateInfo;
  status: SandboxStatus;
  creationTimestamp: string;
  timeoutSeconds?: number;
  startupTimeoutSeconds?: number;
  network?: NetworkSpec;
  rootfsSnapshotSources?: RootfsSnapshotSource[] | null;
}

export interface CreateCommandRequest {
  args: string[];
  cwd?: string;
  env?: Record<string, string>;
  timeoutSeconds?: number;
}

export interface CreateCommandResponse {
  id: string;
}

export interface CommandStatusResponse {
  exitCode: number | null;
}

export interface CreateRootfsSnapshotRequest {
  sandboxId: string;
  snapshotName: string;
  containerName?: string;
  timeoutSeconds?: number;
  ttlSecondsAfterFinished?: number;
}

export type SnapshotStatus = "pending" | "inProgress" | "complete" | "failed";

export interface RootfsSnapshotResponse {
  id: string;
  sandboxId: string;
  snapshotName: string;
  status: SnapshotStatus;
  creationTimestamp: string;
  containerName?: string;
  timeoutSeconds?: number;
  ttlSecondsAfterFinished?: number;
}

export interface FilesystemWriteResponse {
  absolutePath: string;
  bytesWritten: number;
}

export interface ApiError {
  status: number;
  title: string;
  detail?: string;
}
