import { useMutation } from '@tanstack/react-query'
import { filesystemApi } from '@/api/filesystem'

export function useUploadFile(sandboxId: string) {
  return useMutation({
    mutationFn: ({ path, file }: { path: string; file: File }) =>
      filesystemApi.upload(sandboxId, path, file),
  })
}

export function useDownloadFile(sandboxId: string) {
  return useMutation({
    mutationFn: (path: string) => filesystemApi.download(sandboxId, path),
    onSuccess: (blob, path) => {
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = path.split('/').pop() ?? 'download'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
    },
  })
}
