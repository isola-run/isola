import { getApiClient } from './client'
import type { CreateCommandRequest, CreateCommandResponse, CommandStatusResponse } from './types'

export const commandApi = {
  create: async (sandboxId: string, req: CreateCommandRequest): Promise<CreateCommandResponse> => {
    return getApiClient().request<CreateCommandResponse>(
      `/sandboxes/${sandboxId}/commands`,
      { method: 'POST', body: JSON.stringify(req) },
    )
  },

  getStatus: async (sandboxId: string, cmdId: string): Promise<CommandStatusResponse> => {
    return getApiClient().request<CommandStatusResponse>(
      `/sandboxes/${sandboxId}/commands/${cmdId}/status`,
    )
  },

  streamStdout: async (sandboxId: string, cmdId: string, offset: number, signal?: AbortSignal): Promise<Response> => {
    return getApiClient().stream(
      `/sandboxes/${sandboxId}/commands/${cmdId}/stdout?offset=${offset}`,
      { signal },
    )
  },

  streamStderr: async (sandboxId: string, cmdId: string, offset: number, signal?: AbortSignal): Promise<Response> => {
    return getApiClient().stream(
      `/sandboxes/${sandboxId}/commands/${cmdId}/stderr?offset=${offset}`,
      { signal },
    )
  },

  writeStdin: async (sandboxId: string, cmdId: string, data: string): Promise<void> => {
    await getApiClient().upload(
      `/sandboxes/${sandboxId}/commands/${cmdId}/stdin`,
      new Blob([data]),
    )
  },

  kill: async (sandboxId: string, cmdId: string): Promise<void> => {
    await getApiClient().request<void>(
      `/sandboxes/${sandboxId}/commands/${cmdId}`,
      { method: 'DELETE' },
    )
  },
}
