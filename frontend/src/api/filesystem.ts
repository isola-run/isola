import { getApiClient } from './client'
import type { FilesystemWriteResponse } from './types'

export const filesystemApi = {
  upload: async (sandboxId: string, path: string, file: File): Promise<FilesystemWriteResponse> => {
    const buffer = await file.arrayBuffer()
    const res = await getApiClient().upload(
      `/sandboxes/${sandboxId}/filesystem?path=${encodeURIComponent(path)}`,
      buffer,
    )
    return res.json() as Promise<FilesystemWriteResponse>
  },

  download: async (sandboxId: string, path: string): Promise<Blob> => {
    const res = await getApiClient().download(
      `/sandboxes/${sandboxId}/filesystem?path=${encodeURIComponent(path)}`,
    )
    return res.blob()
  },
}
