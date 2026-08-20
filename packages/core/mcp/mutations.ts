import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { WorkspaceMCPServerRequest } from "../types";
import { mcpKeys } from "./queries";

export function useCreateWorkspaceMCPServer() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: WorkspaceMCPServerRequest) => api.createWorkspaceMCPServer(data),
    onSettled: () => queryClient.invalidateQueries({ queryKey: mcpKeys.catalog(workspaceId) }),
  });
}

export function useUpdateWorkspaceMCPServer() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: WorkspaceMCPServerRequest }) => api.updateWorkspaceMCPServer(id, data),
    onSettled: () => queryClient.invalidateQueries({ queryKey: mcpKeys.catalog(workspaceId) }),
  });
}

export function useDeleteWorkspaceMCPServer() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkspaceMCPServer(id),
    onSettled: () => queryClient.invalidateQueries({ queryKey: mcpKeys.all(workspaceId) }),
  });
}

export function useSetAgentMCPAssignments(agentId: string) {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (serverIds: string[]) => api.setAgentMCPAssignments(agentId, serverIds),
    onSettled: () => queryClient.invalidateQueries({ queryKey: mcpKeys.assignments(workspaceId, agentId) }),
  });
}
