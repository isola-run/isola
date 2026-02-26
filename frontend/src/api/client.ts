import { ApiError } from './errors'

export class ApiClient {
  private baseUrl: string

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl
  }

  async request<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`
    const headers: Record<string, string> = { ...init?.headers as Record<string, string> }
    // Only set Content-Type for requests with a body
    if (init?.body) {
      headers['Content-Type'] ??= 'application/json'
    }
    const res = await fetch(url, {
      ...init,
      headers,
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

  async upload(path: string, body: BodyInit): Promise<Response> {
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
