/**
 * Per-session chat realtime — Layer 3.
 *
 * Mounted by the chat screen with the active session id; cleans up on
 * navigate-away. All handlers self-gate on `chat_session_id === sessionId`
 * so a backgrounded session (user switched sessions in the sheet but kept
 * the chat tab open) doesn't keep mutating caches it no longer owns.
 *
 * Events handled:
 *   - chat:message              → invalidate messages + pendingRun
 *   - chat:done                 → patch messages inline + clear pendingRun
 *   - run:queued / dispatch    → seed / promote pendingRun
 *   - run:cancelled            → clear pendingRun
 *   - run:completed            → no-op for messages (chat:done already
 *                                  wrote the assistant message); just
 *                                  defensive clear of pendingRun
 *   - run:failed               → clear pendingRun + invalidate messages
 *                                  (FailRun persists a failure assistant
 *                                  message that must show up)
 *   - chat:session_deleted      → fire onSessionDeleted() so the screen
 *                                  can drop the active id and unwind UI
 *   - reconnect                 → invalidate this session's messages +
 *                                  pendingRun
 */
import { useQueryClient } from "@tanstack/react-query";
import { chatKeys } from "@/data/queries/chat";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  appendRunMessage,
  applyChatDoneToCache,
  clearPendingRun,
  promotePendingRunToRunning,
  seedPendingRunFromQueued,
} from "./chat-ws-updaters";

export function useChatSessionRealtime(
  sessionId: string | null,
  onSessionDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws) => {
      if (!sessionId) return;

      const isMine = (p: { chat_session_id?: string }) =>
        p.chat_session_id === sessionId;

      const invalidateMine = () => {
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.pendingRun(sessionId) });
      };

      return [
        // User-message echo from another device; we may also receive our own
        // sends echoed back, but the id-dedupe in the cache write handles
        // that. Invalidate is cheap — chat:message is rare in practice.
        ws.on("chat:message", (payload) => {
          if (!isMine(payload)) return;
          qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
          qc.invalidateQueries({ queryKey: chatKeys.pendingRun(sessionId) });
        }),
        ws.on("chat:done", (payload) => {
          if (!isMine(payload)) return;
          applyChatDoneToCache(qc, payload);
        }),
        ws.on("run:queued", (payload) => {
          if (!isMine(payload)) return;
          seedPendingRunFromQueued(qc, payload);
        }),
        ws.on("run:dispatch", (payload) => {
          if (!isMine(payload)) return;
          promotePendingRunToRunning(qc, payload);
        }),
        ws.on("run:cancelled", (payload) => {
          if (!isMine(payload)) return;
          clearPendingRun(qc, sessionId);
        }),
        ws.on("run:completed", (payload) => {
          if (!isMine(payload)) return;
          // `chat:done` already wrote the assistant message and cleared
          // pendingRun. Defensive clear in case the two events arrive
          // out of order on a flaky network.
          clearPendingRun(qc, sessionId);
        }),
        ws.on("run:failed", (payload) => {
          if (!isMine(payload)) return;
          // FailRun persists a destructive assistant message — surface it
          // by refetching messages and clearing the pending pill.
          clearPendingRun(qc, sessionId);
          qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        }),
        ws.on("chat:session_deleted", (payload) => {
          if (!isMine(payload)) return;
          onSessionDeleted?.();
        }),
        // Live execution trace for any run firing under this chat session.
        // Per-record gate: `chat_session_id` is optional on the payload
        // (task runs also fire `run:message`), so non-chat traffic is
        // filtered out here. The cache is keyed on `run_id` rather than
        // `sessionId`, so completed runs still render their trace under the
        // assistant bubble after the live pill unmounts.
        ws.on("run:message", (payload) => {
          if (!isMine(payload)) return;
          appendRunMessage(qc, payload);
        }),
        ws.onReconnect(invalidateMine),
      ];
    },
    [sessionId, qc, onSessionDeleted],
  );
}
