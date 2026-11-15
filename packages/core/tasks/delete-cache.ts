import { agentRunsKeys } from "../agent-runs/queries";
import type {
  InfiniteData,
  QueryClient,
  QueryKey,
} from "@tanstack/react-query";
import { agentActivityKeys, agentRunCountsKeys, agentRunSnapshotKeys } from "../agents/queries";
import { labelKeys } from "../labels/queries";
import type {
  Task,
  ListTasksCache,
  ListTasksResponse,
} from "../types";
import { findTaskLocation, removeTaskFromBuckets } from "./cache-helpers";
import { taskKeys } from "./queries";
import { useRecentTasksStore } from "./stores/recent-tasks-store";

export type DeletedTaskCacheMetadata = {
  parentTaskIds: string[];
};

function collectParentId(
  parentTaskIds: Set<string>,
  parentId: string | null | undefined,
) {
  if (parentId) parentTaskIds.add(parentId);
}

function collectParentFromListCache(
  parentTaskIds: Set<string>,
  data: ListTasksCache | undefined,
  taskId: string,
) {
  const parentId = data
    ? findTaskLocation(data, taskId)?.task.parent_task_id
    : undefined;
  collectParentId(parentTaskIds, parentId);
}

function parentIdFromChildrenKey(key: QueryKey) {
  const parentId = key[key.length - 1];
  return typeof parentId === "string" ? parentId : null;
}

export function collectDeletedTaskCacheMetadata(
  qc: QueryClient,
  wsId: string,
  taskId: string,
): DeletedTaskCacheMetadata {
  const parentTaskIds = new Set<string>();

  const detail = qc.getQueryData<Task>(taskKeys.detail(wsId, taskId));
  collectParentId(parentTaskIds, detail?.parent_task_id);

  for (const [, data] of qc.getQueriesData<ListTasksCache>({
    queryKey: taskKeys.list(wsId),
  })) {
    collectParentFromListCache(parentTaskIds, data, taskId);
  }

  for (const [, data] of qc.getQueriesData<
    InfiniteData<ListTasksResponse, number>
  >({ queryKey: taskKeys.flatAll(wsId) })) {
    for (const page of data?.pages ?? []) {
      collectParentId(
        parentTaskIds,
        page.tasks.find((task) => task.id === taskId)?.parent_task_id,
      );
    }
  }

  for (const [, data] of qc.getQueriesData<ListTasksCache>({
    queryKey: taskKeys.myAll(wsId),
  })) {
    collectParentFromListCache(parentTaskIds, data, taskId);
  }

  for (const [key, data] of qc.getQueriesData<Task[]>({
    queryKey: [...taskKeys.all(wsId), "children"],
  })) {
    const child = data?.find((task) => task.id === taskId);
    if (!child) continue;
    collectParentId(parentTaskIds, child.parent_task_id);
    collectParentId(parentTaskIds, parentIdFromChildrenKey(key));
  }

  return { parentTaskIds: Array.from(parentTaskIds) };
}

export function pruneDeletedTaskFromListCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  for (const [key] of qc.getQueriesData<ListTasksCache>({
    queryKey: taskKeys.list(wsId),
  })) {
    qc.setQueryData<ListTasksCache>(key, (old) =>
      old ? removeTaskFromBuckets(old, taskId) : old,
    );
  }

  for (const [key] of qc.getQueriesData<ListTasksCache>({
    queryKey: taskKeys.myAll(wsId),
  })) {
    qc.setQueryData<ListTasksCache>(key, (old) =>
      old ? removeTaskFromBuckets(old, taskId) : old,
    );
  }

  for (const [key, data] of qc.getQueriesData<
    InfiniteData<ListTasksResponse, number>
  >({ queryKey: taskKeys.flatAll(wsId) })) {
    if (!data?.pages) continue;
    const found = data.pages.some((page) =>
      page.tasks.some((task) => task.id === taskId),
    );
    if (!found) continue;
    qc.setQueryData<InfiniteData<ListTasksResponse, number>>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        total: Math.max(0, page.total - 1),
        tasks: page.tasks.filter((task) => task.id !== taskId),
      })),
    });
  }
}

export function pruneDeletedTaskFromParentChildrenCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  metadata: DeletedTaskCacheMetadata,
) {
  for (const parentId of metadata.parentTaskIds) {
    qc.setQueryData<Task[]>(taskKeys.children(wsId, parentId), (old) =>
      old?.filter((task) => task.id !== taskId),
    );
  }
}

export function invalidateDeletedTaskParentCaches(
  qc: QueryClient,
  wsId: string,
  metadata: DeletedTaskCacheMetadata,
) {
  if (metadata.parentTaskIds.length === 0) return;
  for (const parentId of metadata.parentTaskIds) {
    qc.invalidateQueries({ queryKey: taskKeys.children(wsId, parentId) });
  }
  qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) });
}

export function invalidateDeletedTaskDependentCaches(
  qc: QueryClient,
  wsId: string,
) {
  qc.invalidateQueries({ queryKey: agentRunSnapshotKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: agentActivityKeys.last30d(wsId) });
  qc.invalidateQueries({ queryKey: agentRunCountsKeys.last30d(wsId) });
  qc.invalidateQueries({ queryKey: agentRunsKeys.all(wsId) });
}

export function invalidateTaskScopedCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) });
  qc.invalidateQueries({ queryKey: taskKeys.reactions(taskId) });
  qc.invalidateQueries({ queryKey: taskKeys.subscribers(taskId) });
  qc.invalidateQueries({ queryKey: taskKeys.usage(taskId) });
  qc.invalidateQueries({ queryKey: taskKeys.attachments(taskId) });
  qc.invalidateQueries({ queryKey: agentRunsKeys.forTask(wsId, taskId) });
  qc.invalidateQueries({ queryKey: taskKeys.children(wsId, taskId) });
  qc.invalidateQueries({ queryKey: labelKeys.byTask(wsId, taskId) });
}

export function cleanupDeletedTaskCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  metadata = collectDeletedTaskCacheMetadata(qc, wsId, taskId),
) {
  pruneDeletedTaskFromListCaches(qc, wsId, taskId);
  pruneDeletedTaskFromParentChildrenCaches(qc, wsId, taskId, metadata);
  invalidateDeletedTaskParentCaches(qc, wsId, metadata);

  qc.removeQueries({ queryKey: taskKeys.detail(wsId, taskId) });
  qc.removeQueries({ queryKey: taskKeys.timeline(taskId) });
  qc.removeQueries({ queryKey: taskKeys.reactions(taskId) });
  qc.removeQueries({ queryKey: taskKeys.subscribers(taskId) });
  qc.removeQueries({ queryKey: taskKeys.usage(taskId) });
  qc.removeQueries({ queryKey: taskKeys.attachments(taskId) });
  qc.removeQueries({ queryKey: agentRunsKeys.forTask(wsId, taskId) });
  qc.removeQueries({ queryKey: taskKeys.children(wsId, taskId) });
  qc.removeQueries({ queryKey: labelKeys.byTask(wsId, taskId) });

  qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.flatAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
  // Project Gantt cache lives outside `myAll`, so it needs an explicit
  // refresh when an task is removed — the deleted row may have been a
  // scheduled bar visible right now.
  qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
  invalidateDeletedTaskDependentCaches(qc, wsId);

  // Recent Tasks store persists to localStorage and survives reloads, so a
  // deleted id left behind keeps the Cmd+K command bar firing 404s on every
  // open. Both the delete mutation and the WS delete event flow through here,
  // so a single call covers self-delete and cross-client delete.
  useRecentTasksStore.getState().forgetTask(wsId, taskId);
}
