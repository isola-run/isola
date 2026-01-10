import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { api } from '@/services/api';
import type { CreateSandboxRequest, ListSandboxesParams, ExecuteCommandRequest } from '@/types/sandbox';

export function useSandboxes(params: ListSandboxesParams = {}) {
  return useQuery({
    queryKey: ['sandboxes', params],
    queryFn: () => api.listSandboxes(params),
    refetchInterval: 5000,
  });
}

export function useSandbox(id: string) {
  return useQuery({
    queryKey: ['sandbox', id],
    queryFn: () => api.getSandbox(id),
    enabled: !!id,
    refetchInterval: 3000,
  });
}

export function useCreateSandbox() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateSandboxRequest) => api.createSandbox(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sandboxes'] });
    },
  });
}

export function useTerminateSandbox() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, force = false }: { id: string; force?: boolean }) =>
      api.terminateSandbox(id, force),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sandboxes'] });
    },
  });
}

export function useExecuteCommand(sandboxId: string) {
  return useMutation({
    mutationFn: (data: ExecuteCommandRequest) => api.executeCommand(sandboxId, data),
  });
}

export function useUploadFile(sandboxId: string) {
  return useMutation({
    mutationFn: ({ file, path }: { file: File; path: string }) =>
      api.uploadFile(sandboxId, file, path),
  });
}

export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: () => api.getHealth(),
    refetchInterval: 30000,
  });
}
