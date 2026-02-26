import { useQuery, useMutation } from '@tanstack/react-query'
import { commandApi } from '@/api/commands'
import type { CreateCommandRequest } from '@/api/types'

export const commandKeys = {
  status: (sandboxId: string, cmdId: string) =>
    ['sandboxes', sandboxId, 'commands', cmdId, 'status'] as const,
}

export function useCommandStatus(sandboxId: string, cmdId: string, enabled: boolean) {
  return useQuery({
    queryKey: commandKeys.status(sandboxId, cmdId),
    queryFn: () => commandApi.getStatus(sandboxId, cmdId),
    enabled,
    refetchInterval: (query) => {
      if (query.state.data?.exitCode !== null && query.state.data?.exitCode !== undefined) {
        return false
      }
      return 1_000
    },
  })
}

export function useCreateCommand(sandboxId: string) {
  return useMutation({
    mutationFn: (req: CreateCommandRequest) => commandApi.create(sandboxId, req),
  })
}

export function useKillCommand(sandboxId: string) {
  return useMutation({
    mutationFn: (cmdId: string) => commandApi.kill(sandboxId, cmdId),
  })
}

export function useWriteStdin(sandboxId: string) {
  return useMutation({
    mutationFn: ({ cmdId, data }: { cmdId: string; data: string }) =>
      commandApi.writeStdin(sandboxId, cmdId, data),
  })
}
