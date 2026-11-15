import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const agentRunsKeys = {
  all: (wsId: string) => ["agent-runs", wsId] as const,
  forTask: (wsId: string, taskId: string) =>
    [...agentRunsKeys.all(wsId), "task", taskId] as const,
  detail: (wsId: string, agentId: string) =>
    [...agentRunsKeys.all(wsId), "agent", agentId] as const,
};

// All runs for a single agent (the agent detail page consumer). Powers both
// the inspector's 7-day throughput stats and the Tasks tab list — shared so
// they don't fetch twice. WS run events invalidate this via the existing
// run-prefix invalidation in useRealtimeSync.
export function agentRunsOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: agentRunsKeys.detail(wsId, agentId),
    queryFn: () => api.listAgentRuns(agentId),
    staleTime: 30 * 1000,
    gcTime: 5 * 60 * 1000,
    refetchOnWindowFocus: true,
  });
}
