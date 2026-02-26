import { getApiClient } from './client'
import type { CreateSandboxRequest, SandboxResponse, SandboxListResponse } from './types'

export const sandboxApi = {
  list: async (): Promise<SandboxResponse[]> => {
    const data = await getApiClient().request<SandboxListResponse>('/sandboxes')
    return data.sandboxes ?? []
  },

  get: async (id: string): Promise<SandboxResponse> => {
    return getApiClient().request<SandboxResponse>(`/sandboxes/${id}`)
  },

  create: async (req: CreateSandboxRequest): Promise<SandboxResponse> => {
    return getApiClient().request<SandboxResponse>('/sandboxes', {
      method: 'POST',
      body: JSON.stringify(req),
    })
  },

  delete: async (id: string): Promise<void> => {
    await getApiClient().request<void>(`/sandboxes/${id}`, {
      method: 'DELETE',
    })
  },
}
