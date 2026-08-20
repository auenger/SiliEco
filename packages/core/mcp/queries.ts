import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const mcpKeys = {
  all: (workspaceId: string) => ["workspace-mcp", workspaceId] as const,
  catalog: (workspaceId: string) => [...mcpKeys.all(workspaceId), "catalog"] as const,
  assignments: (workspaceId: string, agentId: string) =>
    [...mcpKeys.all(workspaceId), "agent", agentId] as const,
};

export function workspaceMCPServersOptions(workspaceId: string) {
  return queryOptions({
    queryKey: mcpKeys.catalog(workspaceId),
    queryFn: () => api.listWorkspaceMCPServers(),
    enabled: Boolean(workspaceId),
  });
}

export function agentMCPAssignmentsOptions(workspaceId: string, agentId: string) {
  return queryOptions({
    queryKey: mcpKeys.assignments(workspaceId, agentId),
    queryFn: () => api.getAgentMCPAssignments(agentId),
    enabled: Boolean(workspaceId && agentId),
  });
}
