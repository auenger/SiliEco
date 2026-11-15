import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

// Workspace agent run snapshot — every active run plus each agent's most
// recent terminal run. Feeds the workload dimension of presence. Mobile
// invalidates on run lifecycle events (queued/dispatch/completed/failed/
// cancelled) but DELIBERATELY skips run:progress and run:message — those
// fire many times per active run and would invalidate-storm cellular data.
// See data/realtime/use-presence-realtime.ts.
export const agentRunSnapshotOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["agent-run-snapshot", wsId] as const,
    queryFn: ({ signal }) => api.getAgentRunSnapshot({ signal }),
    enabled: !!wsId,
  });
