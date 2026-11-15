import type { QueryClient } from "@tanstack/react-query";
import { inboxKeys } from "./queries";
import type { InboxItem, TaskStatus } from "../types";

export function onInboxNew(
  qc: QueryClient,
  wsId: string,
  _item: InboxItem,
) {
  // Use invalidateQueries instead of setQueryData — triggers a refetch that
  // reliably notifies all observers. The inbox list is small so this is cheap.
  //
  // Both lists: a new notification on an ARCHIVED task puts that task back in
  // the main inbox, which means it must also leave the archived list. The
  // server owns that split (ListArchivedInboxItems excludes tasks with an
  // active row), so refetching both is what keeps them mutually exclusive.
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

export function patchInboxTaskStatus(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  status: TaskStatus,
) {
  const patch = (old: InboxItem[] | undefined) =>
    old?.map((i) => (i.task_id === taskId ? { ...i, task_status: status } : i));
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), patch);
  // Archived rows render the same status icon, so they need the same patch.
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), patch);
}

export function onInboxTaskStatusChanged(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  status: TaskStatus,
) {
  patchInboxTaskStatus(qc, wsId, taskId, status);
}

// Mirrors the DB-level ON DELETE CASCADE on inbox_item.task_id: when an task
// is deleted, all inbox items that referenced it are gone server-side, so drop
// them from the cache too — from the archived list as well, which holds rows
// for the same tasks.
export function onInboxTaskDeleted(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  const drop = (old: InboxItem[] | undefined) =>
    old?.filter((i) => i.task_id !== taskId);
  qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), drop);
  qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), drop);
}

// Refresh both the main and archived lists. Every inbox event can move an item
// across that boundary (archive, unarchive, or a new notification reviving an
// archived task), and the split is decided server-side, so the two are always
// invalidated together.
export function onInboxInvalidate(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
}

// Refresh the cross-workspace unread summary (workspace-switcher dot). The
// summary spans every workspace, so it is invalidated on ANY inbox event
// regardless of which workspace the event came from — including read/archive
// events from a workspace other than the active one, which the workspace-
// scoped list invalidation cannot reach.
export function onInboxSummaryInvalidate(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}
