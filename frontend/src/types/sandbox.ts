export type SandboxState =
  | 'pending'
  | 'starting'
  | 'running'
  | 'terminating'
  | 'stopped'
  | 'error'
  | 'unknown';

export interface Sandbox {
  id: string;
  name: string;
  state: SandboxState;
  desiredState: SandboxState;
  env?: Record<string, string>;
  labels?: Record<string, string>;
  errorReason?: string | null;
  createdAt: string;
}

export interface SandboxListResponse {
  items: Sandbox[];
  total: number;
  limit: number;
  offset: number;
}

export interface CreateSandboxRequest {
  name: string;
  image?: string;
  region?: string;
  cpu?: number;
  memory?: number;
  disk?: number;
  gpu?: number;
  env?: Record<string, string>;
  labels?: Record<string, string>;
  autoStart?: boolean;
}

export interface ExecuteCommandRequest {
  command: string;
}

export interface ExecuteCommandResponse {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface FileUploadResponse {
  success: boolean;
  path: string;
  size: number;
}

export interface UploadUrlRequest {
  path: string;
  filename: string;
  content_type?: string;
}

export interface UploadUrlResponse {
  upload_url: string;
  upload_id: string;
  expires_in: number;
}

export interface ConfirmUploadRequest {
  upload_id: string;
  filename: string;
  path: string;
}

export interface HealthResponse {
  status: string;
  timestamp: string;
  components: Record<string, string>;
  version: string;
}

export interface ApiError {
  error: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface ListSandboxesParams {
  state?: SandboxState;
  limit?: number;
  offset?: number;
}
