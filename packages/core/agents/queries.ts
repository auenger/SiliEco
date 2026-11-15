import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  WorkspaceWorkingAgentMineRelation,
  WorkspaceWorkingAgentType,
} from "../types";

export const agentRunSnapshotKeys = {
  all: (wsId: string) => ["workspaces", wsId, "agent-run-snapshot"] as const,
  list: (wsId: string) => [...agentRunSnapshotKeys.all(wsId), "list"] as const,
};

export const workspaceWorkingAgentsKeys = {
  all: (wsId: string) => ["workspaces", wsId, "working-agents"] as const,
  list: (
    wsId: string,
    type?: WorkspaceWorkingAgentType,
    mineRelation?: WorkspaceWorkingAgentMineRelation,
    parentTaskId?: string,
  ) =>
    [
      ...workspaceWorkingAgentsKeys.all(wsId),
      "list",
      type ?? "all",
      mineRelation
        ? `mine:${mineRelation}`
        : parentTaskId
          ? `parent:${parentTaskId}`
          : "workspace",
    ] as const,
};

export const agentActivityKeys = {
  all: (wsId: string) => ["workspaces", wsId, "agent-activity"] as const,
  last30d: (wsId: string) => [...agentActivityKeys.all(wsId), "30d"] as const,
};

export const agentRunCountsKeys = {
  all: (wsId: string) => ["workspaces", wsId, "agent-run-counts"] as const,
  last30d: (wsId: string) => [...agentRunCountsKeys.all(wsId), "30d"] as const,
};

// Workspace-scoped agent run snapshot — every active run plus each agent's
// most recent terminal run. This is the single shared source of truth that
// powers per-agent presence derivation across the app. One fetch per
// workspace; all agent dots / hover cards / list rows derive presence from
// this cache with zero additional network traffic.
//
// Presence itself is derived from the active runs only (see derive-presence.ts
// and #1823). The one terminal row per agent is used solely for the Squad hover
// card's "last activity" line; SILI-5436 tracks moving it to a dedicated lazy
// endpoint so this hot query stops carrying history at all.
//
// The 30s staleTime is a safety net only; the primary freshness signal is
// WS run events, which invalidate this query immediately. Without WS,
// presence still updates within 30s on focus / mount.
export function agentRunSnapshotOptions(wsId: string) {
  return queryOptions({
    queryKey: agentRunSnapshotKeys.list(wsId),
    queryFn: () => api.getAgentRunSnapshot(),
    staleTime: 30 * 1000,
    gcTime: 5 * 60 * 1000,
    refetchOnWindowFocus: true,
  });
}

// Working-agent summaries, optionally narrowed to a My Tasks relation or to
// one task's direct children. Task lifecycle WebSocket events invalidate
// every narrowing immediately; the short stale time is the reconnect /
// missed-event safety net.
export function workspaceWorkingAgentsOptions(
  wsId: string,
  type?: WorkspaceWorkingAgentType,
  mineRelation?: WorkspaceWorkingAgentMineRelation,
  parentTaskId?: string,
) {
  return queryOptions({
    queryKey: workspaceWorkingAgentsKeys.list(
      wsId,
      type,
      mineRelation,
      parentTaskId,
    ),
    queryFn: () =>
      api.getWorkspaceWorkingAgents(type, mineRelation, parentTaskId),
    staleTime: 30 * 1000,
    gcTime: 5 * 60 * 1000,
    refetchOnWindowFocus: true,
  });
}

// Workspace-wide daily run activity for the last 30 days, anchored on
// completed_at. One fetch backs both the Agents-list sparkline (which
// only uses the trailing 7 buckets via `summarizeActivityWindow`) and
// the agent detail "Last 30 days" panel. WS run lifecycle events
// invalidate this query in useRealtimeSync; the staleTime is a
// tab-focus safety net.
export function agentActivity30dOptions(wsId: string) {
  return queryOptions({
    queryKey: agentActivityKeys.last30d(wsId),
    queryFn: () => api.getWorkspaceAgentActivity30d(),
    staleTime: 60 * 1000,
    gcTime: 5 * 60 * 1000,
    refetchOnWindowFocus: true,
  });
}

// Workspace-wide 30-day run counts for the Agents-list RUNS column. Same
// single-fetch / WS-invalidate pattern as activity24hOptions.
export function agentRunCounts30dOptions(wsId: string) {
  return queryOptions({
    queryKey: agentRunCountsKeys.last30d(wsId),
    queryFn: () => api.getWorkspaceAgentRunCounts(),
    staleTime: 60 * 1000,
    gcTime: 5 * 60 * 1000,
    refetchOnWindowFocus: true,
  });
}

// Agent templates are workspace-independent: a static catalog served from
// the server's embedded JSON. Cache effectively forever — the only way the
// list / detail change is a server deploy, and a hard reload picks that up.
export const agentTemplateKeys = {
  all: () => ["agent-templates"] as const,
  list: () => [...agentTemplateKeys.all(), "list"] as const,
  detail: (slug: string) => [...agentTemplateKeys.all(), "detail", slug] as const,
};

export function agentTemplateListOptions() {
  return queryOptions({
    queryKey: agentTemplateKeys.list(),
    queryFn: () => api.listAgentTemplates(),
    staleTime: Infinity,
    gcTime: 30 * 60 * 1000,
  });
}

export function agentTemplateDetailOptions(slug: string) {
  return queryOptions({
    queryKey: agentTemplateKeys.detail(slug),
    queryFn: () => api.getAgentTemplate(slug),
    staleTime: Infinity,
    gcTime: 30 * 60 * 1000,
  });
}
