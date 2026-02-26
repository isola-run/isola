import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { sandboxApi } from '@/api/sandboxes'
import type { CreateSandboxRequest, SandboxResponse } from '@/api/types'

export const sandboxKeys = {
  all: ['sandboxes'] as const,
  detail: (id: string) => ['sandboxes', id] as const,
}

export function useSandboxes() {
  return useQuery({
    queryKey: sandboxKeys.all,
    queryFn: sandboxApi.list,
    staleTime: 5_000,
    refetchInterval: 5_000,
  })
}

export function useSandbox(id: string) {
  return useQuery({
    queryKey: sandboxKeys.detail(id),
    queryFn: () => sandboxApi.get(id),
    staleTime: 3_000,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      if (status === 'creating' || status === 'shuttingDown') return 2_000
      if (status === 'running') return 5_000
      return false
    },
  })
}

export function useCreateSandbox() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (req: CreateSandboxRequest) => sandboxApi.create(req),
    onSuccess: (newSandbox: SandboxResponse) => {
      queryClient.invalidateQueries({ queryKey: sandboxKeys.all })
      queryClient.setQueryData(sandboxKeys.detail(newSandbox.id), newSandbox)
    },
  })
}

export function useDeleteSandbox() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => sandboxApi.delete(id),
    onSuccess: (_data, id) => {
      queryClient.invalidateQueries({ queryKey: sandboxKeys.all })
      queryClient.removeQueries({ queryKey: sandboxKeys.detail(id) })
    },
  })
}
