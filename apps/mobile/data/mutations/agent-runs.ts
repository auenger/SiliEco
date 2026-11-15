import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { AgentRun } from "@silieco/core/types";
import { api } from "@/data/api";
import { agentRunsKeys } from "@/data/queries/agent-runs";
import { useWorkspaceStore } from "@/data/workspace-store";

export function useCancelRun(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (runId: string) => api.cancelRunById(runId),
    onMutate: async (runId) => {
      const activeKey = agentRunsKeys.activeForTask(wsId, taskId);
      await qc.cancelQueries({ queryKey: activeKey });
      const prev = qc.getQueryData<AgentRun[]>(activeKey);
      qc.setQueryData<AgentRun[]>(activeKey, (old) =>
        old ? old.filter((t) => t.id !== runId) : old,
      );
      return { prev, activeKey };
    },
    onError: (_err, _runId, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.activeKey, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: agentRunsKeys.activeForTask(wsId, taskId) });
      qc.invalidateQueries({ queryKey: agentRunsKeys.historyForTask(wsId, taskId) });
    },
  });
}
