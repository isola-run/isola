import { ApiError } from './errors'

export class ApiClient {
  constructor(private baseUrl: string) {}

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        ...init?.headers,
      },
    })

    if (!res.ok) {
      throw await ApiError.fromResponse(res)
    }

    if (res.status === 204) return undefined as T
    return res.json() as Promise<T>
  }

  async stream(path: string, init?: RequestInit): Promise<Response> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url, init)
    if (!res.ok) {
      throw await ApiError.fromResponse(res)
    }
    return res
  }

  async upload(path: string, body: Blob | ArrayBuffer): Promise<Response> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body,
    })
    if (!res.ok) {
      throw await ApiError.fromResponse(res)
    }
    return res
  }

  async download(path: string): Promise<Response> {
    const url = `${this.baseUrl}${path}`
    const res = await fetch(url)
    if (!res.ok) {
      throw await ApiError.fromResponse(res)
    }
    return res
  }
}

let clientInstance: ApiClient | null = null

export function getApiClient(): ApiClient {
  if (!clientInstance) {
    const baseUrl = localStorage.getItem('isola-api-url') || '/api'
    clientInstance = new ApiClient(baseUrl)
  }
  return clientInstance
}

export function resetApiClient(): void {
  clientInstance = null
}
