/**
 * Mobile inbox cache patchers. Mirrors `packages/core/inbox/ws-updaters.ts`
 * (per CLAUDE.md "Mobile-owned updaters" — copy the design, don't import:
 * key factory binding + cache shape can drift independently).
 *
 * Two cross-cutting events that change inbox state without firing an
 * `inbox:*` event:
 *   - `task:updated` carrying a new status → the inbox row's StatusIcon
 *     must update inline. Without this patch the row keeps showing the
 *     prior status until the next inbox event triggers a full refetch.
 *   - `task:deleted` → all inbox items pointing at that task are gone
 *     server-side through application cleanup; the cache should drop
 *     them too, otherwise tapping an inbox row navigates to a 404 task.
 *
 * Listing-level only; use-inbox-realtime wires these into the WS layer.
 */
import type { QueryClient } from "@tanstack/react-query";
import type { InboxItem, TaskStatus } from "@silieco/core/types";
import { inboxKeys } from "@/data/queries/inbox";

export function patchInboxTaskStatus(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  status: TaskStatus,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.map((i) =>
      i.task_id === taskId ? { ...i, task_status: status } : i,
    ),
  );
}

export function dropInboxItemsByTask(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
    old?.filter((i) => i.task_id !== taskId),
  );
}
