export type SandboxStatus = 'creating' | 'running' | 'shuttingDown' | 'failed' | 'stopped' | 'unknown'

export interface ResourceSpec {
  cpu?: string
  memory?: string
  ephemeralStorage?: string
}

export interface ResourceRequirements {
  limits?: ResourceSpec
  requests?: ResourceSpec
}

export interface ContainerSpec {
  image: string
  command?: string[]
  env?: Record<string, string>
  resources?: ResourceRequirements
}

export interface ContainerInfo {
  image: string
  command?: string[]
  resources?: ResourceRequirements
}

export interface PodTemplateRequest {
  container: ContainerSpec
}

export interface PodTemplateResponse {
  container: ContainerInfo
}

export interface NetworkSpec {
  allowInternetEgress?: boolean
  allowClusterDNS?: boolean
  allowedEgressCIDRs?: string[]
  nameservers?: string[]
}

export interface ShutdownPolicy {
  strategy?: 'Delete' | 'SnapshotRootfs'
  activeDeadlineSeconds?: number
}

export interface CreateSandboxRequest {
  podTemplate: PodTemplateRequest
  activeDeadlineSeconds?: number
  network?: NetworkSpec
  shutdownPolicy?: ShutdownPolicy
}

export interface SandboxResponse {
  id: string
  podTemplate: PodTemplateResponse
  activeDeadlineSeconds?: number
  network?: NetworkSpec
  status: SandboxStatus
  creationTimestamp: string
}

export interface SandboxListResponse {
  sandboxes: SandboxResponse[]
}

export interface CreateCommandRequest {
  cmd: string
  args?: string[]
  env?: Record<string, string>
  cwd?: string
  timeout?: number
}

export interface CreateCommandResponse {
  commandId: string
}

export interface CommandStatusResponse {
  exitCode: number | null
}

export interface FilesystemWriteResponse {
  absolutePath: string
  bytesWritten: number
}

export interface HealthResponse {
  status: string
}

export interface ErrorResponse {
  status: number
  title: string
  detail?: string
}
