import type {
  Sandbox,
  SandboxListResponse,
  CreateSandboxRequest,
  ExecuteCommandRequest,
  ExecuteCommandResponse,
  FileUploadResponse,
  UploadUrlRequest,
  UploadUrlResponse,
  ConfirmUploadRequest,
  HealthResponse,
  ListSandboxesParams,
  ApiError,
} from '@/types/sandbox';

const API_BASE = '/api/v1';

class ApiClient {
  private apiKey: string = '';

  setApiKey(key: string) {
    this.apiKey = key;
    localStorage.setItem('isola_api_key', key);
  }

  getApiKey(): string {
    if (!this.apiKey) {
      this.apiKey = localStorage.getItem('isola_api_key') || '';
    }
    return this.apiKey;
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...(options.headers as Record<string, string>),
    };

    const apiKey = this.getApiKey();
    if (apiKey) {
      headers['X-API-Key'] = apiKey;
    }

    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers,
    });

    if (!response.ok) {
      const error: ApiError = await response.json().catch(() => ({
        error: 'Unknown',
        message: `HTTP ${response.status}: ${response.statusText}`,
      }));
      throw new Error(error.message || error.error);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  async getHealth(): Promise<HealthResponse> {
    const response = await fetch('/health');
    return response.json();
  }

  async listSandboxes(params: ListSandboxesParams = {}): Promise<SandboxListResponse> {
    const searchParams = new URLSearchParams();
    if (params.state) searchParams.set('state', params.state);
    if (params.limit) searchParams.set('limit', params.limit.toString());
    if (params.offset) searchParams.set('offset', params.offset.toString());

    const query = searchParams.toString();
    return this.request<SandboxListResponse>(`/sandboxes${query ? `?${query}` : ''}`);
  }

  async getSandbox(id: string): Promise<Sandbox> {
    return this.request<Sandbox>(`/sandboxes/${id}`);
  }

  async createSandbox(data: CreateSandboxRequest): Promise<Sandbox> {
    return this.request<Sandbox>('/sandboxes', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async terminateSandbox(id: string, force = false): Promise<void> {
    return this.request<void>(`/sandboxes/${id}?force=${force}`, {
      method: 'DELETE',
    });
  }

  async executeCommand(id: string, data: ExecuteCommandRequest): Promise<ExecuteCommandResponse> {
    return this.request<ExecuteCommandResponse>(`/sandboxes/${id}/execute`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async uploadFile(id: string, file: File, path: string): Promise<FileUploadResponse> {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('path', path);

    const response = await fetch(`${API_BASE}/sandboxes/${id}/files`, {
      method: 'POST',
      headers: {
        'X-API-Key': this.getApiKey(),
      },
      body: formData,
    });

    if (!response.ok) {
      const error = await response.json().catch(() => ({ message: 'Upload failed' }));
      throw new Error(error.message);
    }

    return response.json();
  }

  async generateUploadUrl(id: string, data: UploadUrlRequest): Promise<UploadUrlResponse> {
    return this.request<UploadUrlResponse>(`/sandboxes/${id}/files/upload-url`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async confirmUpload(id: string, data: ConfirmUploadRequest): Promise<FileUploadResponse> {
    return this.request<FileUploadResponse>(`/sandboxes/${id}/files/confirm`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }
}

export const api = new ApiClient();
