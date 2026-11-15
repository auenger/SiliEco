/**
 * Workspace Tasks realtime — listing-level subscription. Mounted globally
 * (workspace-session-lifetime) alongside `useMyTasksRealtime` so the
 * workspace-wide list stays fresh regardless of which tab is foregrounded.
 *
 * task:created     — payload includes the full Task object; prepend in
 *                     place. Avoids a refetch and keeps UI snappy when
 *                     someone else creates a task while we're looking
 *                     at the list.
 * task:updated     — patch in-place; client-side filter/grouping
 *                     re-derives on the next render.
 * task:deleted     — strip from the list.
 * task_labels:changed — handled by `patchTaskLabels` in the shared
 *                     updaters (already patches `list(wsId)` too — see
 *                     `task-ws-updaters.ts`). No subscription needed
 *                     here because `useMyTasksRealtime` already drives
 *                     it for every workspace surface.
 * onReconnect       — invalidate `list(wsId)` since we may have missed
 *                     a create/delete while disconnected.
 *
 * This hook is independent of `useMyTasksRealtime` (different cache key
 * `list(wsId)` vs `myAll(wsId)`). Both are listing-level and run in
 * parallel — apps/mobile/CLAUDE.md "Mobile-owned updaters" / "list-level
 * global, per-record per-screen".
 */
import { useQueryClient } from "@tanstack/react-query";
import { taskKeys } from "@/data/queries/task-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  patchTasksList,
  prependToTasksList,
  removeFromTasksList,
} from "./task-ws-updaters";

export function useTasksRealtime() {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      const invalidateList = () =>
        qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });

      return [
        ws.on("task:created", (payload) => {
          prependToTasksList(qc, wsId, payload.task);
        }),
        ws.on("task:updated", (payload) => {
          patchTasksList(qc, wsId, payload.task);
        }),
        ws.on("task:deleted", (payload) => {
          removeFromTasksList(qc, wsId, payload.task_id);
        }),
        ws.onReconnect(invalidateList),
      ];
    },
    [qc],
  );
}
