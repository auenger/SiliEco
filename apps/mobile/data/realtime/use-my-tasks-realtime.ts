/**
 * My Tasks realtime — listing-level subscriptions. Mounted globally
 * (workspace-session-lifetime) alongside useInboxRealtime so the user's
 * task list stays fresh regardless of which tab is foregrounded.
 *
 * task:created     — invalidate myAll(wsId). We don't try to predict
 *                     whether the new task belongs to assigned/created/
 *                     agents scope or matches the user's current filter;
 *                     a fresh fetch is the cheapest correct answer.
 * task:updated     — patch in-place across every cached list entry.
 *                     A status change re-buckets the SectionList on the
 *                     consumer side; we don't have to invalidate.
 * task:deleted     — strip from every cached list entry.
 * task_labels:changed — patch labels via the shared updater
 *                     (also handles the detail cache; harmless if
 *                     the task isn't in any cached list).
 * onReconnect       — invalidate myAll(wsId) since we may have missed
 *                     a create/delete while disconnected.
 *
 * Inbox realtime (use-inbox-realtime.ts) handles its own keys and runs
 * in parallel; the two are independent.
 */
import { useQueryClient } from "@tanstack/react-query";
import { taskKeys } from "@/data/queries/task-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchTaskLabels,
  patchMyTasksList,
  removeFromMyTasksList,
} from "./task-ws-updaters";

export function useMyTasksRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateMyAll = () =>
        qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });

      return [
        // Server is the authority on which scopes/filters a new task lands
        // in — we don't need to read the payload, just refetch.
        ws.on("task:created", () => invalidateMyAll()),
        ws.on("task:updated", (payload) => {
          patchMyTasksList(qc, wsId, payload.task);
        }),
        ws.on("task:deleted", (payload) => {
          removeFromMyTasksList(qc, wsId, payload.task_id);
        }),
        ws.on("task_labels:changed", (payload) => {
          patchTaskLabels(qc, wsId, payload.task_id, payload.labels);
        }),
        ws.onReconnect(invalidateMyAll),
      ];
    },
    [qc],
  );
}
