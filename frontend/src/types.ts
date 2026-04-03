// API types matching the OpenAPI spec

export type SandboxStatus =
  | "creating"
  | "running"
  | "shuttingDown"
  | "failed"
  | "stopped"
  | "unknown";

export type SnapshotStatus = "pending" | "inProgress" | "complete" | "failed";

export interface SandboxSummary {
  id: string;
  status: SandboxStatus;
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

export function isApiError(err: unknown): err is ApiError {
  return (
    typeof err === "object" &&
    err !== null &&
    "status" in err &&
    "title" in err
  );
}

export function getErrorMessage(err: unknown, fallback = "Request failed"): string {
  if (isApiError(err)) return err.detail ?? err.title;
  return fallback;
}
