/** Mobile-owned execution queries; keys mirror the shared agent-runs domain. */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const agentRunsKeys = {
  all: (wsId: string | null) => ["agent-runs", wsId] as const,
  forTask: (wsId: string | null, taskId: string) =>
    [...agentRunsKeys.all(wsId), "task", taskId] as const,
  activeForTask: (wsId: string | null, taskId: string) =>
    [...agentRunsKeys.forTask(wsId, taskId), "active"] as const,
  historyForTask: (wsId: string | null, taskId: string) =>
    [...agentRunsKeys.forTask(wsId, taskId), "history"] as const,
};

// Lifecycle events invalidate these queries because their payload is partial.
export const activeRunsForTaskOptions = (wsId: string | null, taskId: string) =>
  queryOptions({
    queryKey: agentRunsKeys.activeForTask(wsId, taskId),
    queryFn: ({ signal }) => api.getActiveRunsForTask(taskId, { signal }),
    enabled: !!wsId && !!taskId,
  });

export const runsForTaskOptions = (wsId: string | null, taskId: string) =>
  queryOptions({
    queryKey: agentRunsKeys.historyForTask(wsId, taskId),
    queryFn: ({ signal }) => api.listRunsByTask(taskId, { signal }),
    enabled: !!wsId && !!taskId,
  });
