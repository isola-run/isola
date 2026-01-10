import type {
  Sandbox,
  SandboxList,
  CreateSandboxRequest,
  ExecuteCommandResponse,
  FileUploadResponse,
  UploadUrlRequest,
  UploadUrlResponse,
  ConfirmUploadRequest,
  ConfirmUploadResponse,
  HealthResponse,
  ListSandboxesParams,
  ApiError,
} from '@/types'

const API_BASE = '/api/v1'

class ApiClient {
  private apiKey: string | null = null

  setApiKey(key: string | null) {
    this.apiKey = key
    if (key) {
      localStorage.setItem('isola_api_key', key)
    } else {
      localStorage.removeItem('isola_api_key')
    }
  }

  getApiKey(): string | null {
    if (this.apiKey) return this.apiKey
    const stored = localStorage.getItem('isola_api_key')
    if (stored) {
      this.apiKey = stored
    }
    return this.apiKey
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...((options.headers as Record<string, string>) || {}),
    }

    const apiKey = this.getApiKey()
    if (apiKey) {
      headers['X-API-Key'] = apiKey
    }

    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers,
    })

    if (!response.ok) {
      const error: ApiError = await response.json().catch(() => ({
        error: 'Unknown',
        message: `HTTP ${response.status}: ${response.statusText}`,
      }))
      throw new Error(error.message || error.error)
    }

    if (response.status === 204) {
      return undefined as T
    }

    return response.json()
  }

  async health(): Promise<HealthResponse> {
    const response = await fetch('/health')
    return response.json()
  }

  async listSandboxes(params: ListSandboxesParams = {}): Promise<SandboxList> {
    const searchParams = new URLSearchParams()
    if (params.state) searchParams.set('state', params.state)
    if (params.limit) searchParams.set('limit', params.limit.toString())
    if (params.offset) searchParams.set('offset', params.offset.toString())

    const query = searchParams.toString()
    return this.request<SandboxList>(`/sandboxes${query ? `?${query}` : ''}`)
  }

  async getSandbox(id: string): Promise<Sandbox> {
    return this.request<Sandbox>(`/sandboxes/${id}`)
  }

  async createSandbox(data: CreateSandboxRequest): Promise<Sandbox> {
    return this.request<Sandbox>('/sandboxes', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async deleteSandbox(id: string, force = false): Promise<void> {
    const query = force ? '?force=true' : ''
    return this.request<void>(`/sandboxes/${id}${query}`, {
      method: 'DELETE',
    })
  }

  async executeCommand(
    sandboxId: string,
    command: string
  ): Promise<ExecuteCommandResponse> {
    return this.request<ExecuteCommandResponse>(
      `/sandboxes/${sandboxId}/execute`,
      {
        method: 'POST',
        body: JSON.stringify({ command }),
      }
    )
  }

  async uploadFile(
    sandboxId: string,
    file: File,
    path: string
  ): Promise<FileUploadResponse> {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('path', path)

    const headers: Record<string, string> = {}
    const apiKey = this.getApiKey()
    if (apiKey) {
      headers['X-API-Key'] = apiKey
    }

    const response = await fetch(`${API_BASE}/sandboxes/${sandboxId}/files`, {
      method: 'POST',
      headers,
      body: formData,
    })

    if (!response.ok) {
      const error = await response.json().catch(() => ({
        message: `Upload failed: ${response.statusText}`,
      }))
      throw new Error(error.message)
    }

    return response.json()
  }

  async getUploadUrl(
    sandboxId: string,
    data: UploadUrlRequest
  ): Promise<UploadUrlResponse> {
    return this.request<UploadUrlResponse>(
      `/sandboxes/${sandboxId}/files/upload-url`,
      {
        method: 'POST',
        body: JSON.stringify(data),
      }
    )
  }

  async confirmUpload(
    sandboxId: string,
    data: ConfirmUploadRequest
  ): Promise<ConfirmUploadResponse> {
    return this.request<ConfirmUploadResponse>(
      `/sandboxes/${sandboxId}/files/confirm`,
      {
        method: 'POST',
        body: JSON.stringify(data),
      }
    )
  }
}

export const apiClient = new ApiClient()
