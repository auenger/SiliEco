/**
 * Chat query keys + queryOptions factories.
 *
 * Keys:
 *   - sessions(wsId)        → ChatSession[] for the workspace dropdown / sheet
 *   - messages(sessionId)   → ChatMessage[] for the active session
 *   - pendingRun(sessionId)→ ChatPendingRun, populated when an agent run is
 *                             in flight; cleared on chat:done / run:cancelled
 *
 * Same shape as web's `chatKeys` in packages/core/chat/queries.ts (mobile
 * owns its own copy per the "mirror, don't import" rule in apps/mobile/CLAUDE.md).
 *
 * `staleTime: Infinity` everywhere — caches are kept fresh by WS event
 * handlers, not by background refetch. Foreground / reconnect invalidates
 * are scoped to each owning hook (see use-chat-sessions-realtime.ts and
 * use-chat-session-realtime.ts).
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const chatKeys = {
  all: (wsId: string | null) => ["chat", wsId] as const,
  sessions: (wsId: string | null) =>
    [...chatKeys.all(wsId), "sessions"] as const,
  messages: (sessionId: string) => ["chat", "messages", sessionId] as const,
  pendingRun: (sessionId: string) =>
    ["chat", "pending-run", sessionId] as const,
  /** Per-run live execution timeline (thinking / tool_use / tool_result /
   *  text / error rows). Cache is workspace-agnostic — keyed only on
   *  `runId` — matching web's `chatKeys.runMessages` shape so future
   *  cross-feature consumers (task agent cards) can share the cache.
   *  `run:message` WS events append rows in place; once the run
   *  completes the cache stays warm so the persisted assistant message
   *  can render the same trace without refetching. */
  runMessages: (runId: string) => ["run-messages", runId] as const,
};

// UUID gate mirrors `packages/core/chat/queries.ts`: optimistic run ids
// (`optimistic-…`) are not real backend rows, so the query must be
// disabled until we have a server-issued UUID. Returning the cache for
// an optimistic id would 404 the API.
const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isRunMessageRunId(
  runId: string | null | undefined,
): runId is string {
  return typeof runId === "string" && UUID_PATTERN.test(runId);
}

export const chatSessionsOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: chatKeys.sessions(wsId),
    queryFn: ({ signal }) => api.listChatSessions({ signal }),
    enabled: !!wsId,
    staleTime: Infinity,
  });

export const chatMessagesOptions = (sessionId: string | null) =>
  queryOptions({
    queryKey: chatKeys.messages(sessionId ?? ""),
    queryFn: ({ signal }) => api.listChatMessages(sessionId!, { signal }),
    enabled: !!sessionId,
    staleTime: Infinity,
  });

export const pendingChatRunOptions = (sessionId: string | null) =>
  queryOptions({
    queryKey: chatKeys.pendingRun(sessionId ?? ""),
    queryFn: ({ signal }) => api.getPendingChatRun(sessionId!, { signal }),
    enabled: !!sessionId,
    staleTime: Infinity,
  });

export const runMessagesOptions = (runId: string | null | undefined) =>
  queryOptions({
    queryKey: chatKeys.runMessages(runId ?? ""),
    queryFn: ({ signal }) => api.listAgentRunMessages(runId!, { signal }),
    enabled: isRunMessageRunId(runId),
    staleTime: Infinity,
  });
