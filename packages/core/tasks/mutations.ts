import { useState, useCallback } from "react";
import { hashKey, useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { api } from "../api";
import {
  taskKeys,
  TASK_PAGE_SIZE,
  type AssigneeGroupedTasksFilter,
  type TaskSortParam,
  type MyTasksFilter,
} from "./queries";
import { projectKeys } from "../projects/queries";
import { inboxKeys } from "../inbox/queries";
import { workflowKeys } from "../workflows/queries";
import {
  applyTaskChange,
  invalidateTaskDerivatives,
  invalidateStaleListKeys,
  rollbackTaskChange,
  type TaskFlatCache,
  type TaskTableRowCache,
} from "./cache-coordinator";
import { taskChangedDims } from "./surface/membership";
import {
  addTaskToBuckets,
  getBucket,
  setBucket,
} from "./cache-helpers";
import {
  cleanupDeletedTaskCaches,
  collectDeletedTaskCacheMetadata,
  invalidateDeletedTaskDependentCaches,
  invalidateDeletedTaskParentCaches,
  invalidateTaskScopedCaches,
  pruneDeletedTaskFromListCaches,
  pruneDeletedTaskFromParentChildrenCaches,
} from "./delete-cache";
import { useWorkspaceId } from "../hooks";
import { revisionConflictBody, useRevisionConflictStore } from "../revisions";
import { useRecentContextStore } from "../chat/recent-context-store";
import { useRecentTasksStore } from "./stores";
import type { GroupedTasksResponse, InboxItem, Task, TaskAssigneeGroup, TaskReaction, TaskStatus } from "../types";
import type {
  CreateTaskRequest,
  ListTasksCache,
  MoveTaskRequest,
  UpdateTaskRequest,
} from "../types";
import type { TimelineEntry, TaskSubscriber, Reaction } from "../types";
import { sortTimelineEntriesAsc } from "./timeline-sort";

// ---------------------------------------------------------------------------
// Shared mutation variable types — used by both mutation hooks and
// useMutationState consumers to keep the type assertion in sync.
// ---------------------------------------------------------------------------

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  existing: Reaction | undefined;
};

export type ToggleTaskReactionVars = {
  emoji: string;
  existing: TaskReaction | undefined;
};

export type UpdateTaskMutationInput = {
  id: string;
  /**
   * Present only for drag/drop. `position` remains in the optimistic patch,
   * while the request sent to the server contains relative anchors instead.
   */
  move_intent?: Pick<MoveTaskRequest, "before_id" | "after_id">;
} & UpdateTaskRequest;

const MANUAL_REVISION_CONFLICT_FIELDS = [
  "title",
  "description",
  "attachment_ids",
] as const;

function canRetryTaskRevisionConflict(update: UpdateTaskRequest): boolean {
  if (Object.prototype.hasOwnProperty.call(update, "expected_revision")) {
    return false;
  }
  return !MANUAL_REVISION_CONFLICT_FIELDS.some((field) =>
    Object.prototype.hasOwnProperty.call(update, field),
  );
}

// ---------------------------------------------------------------------------
// Per-status pagination
// ---------------------------------------------------------------------------

/**
 * Paginate one status column into the cache. Works for both the workspace
 * task list and per-scope My Tasks lists (pass `myTasks` to target the
 * latter).
 *
 * `sort` must match the sort the consuming `useQuery` was called with —
 * the query key embeds it (see `listSorted` / `myListSorted`), so a load-more
 * with the wrong sort would patch a stale cache entry that nobody is
 * subscribed to. It is also threaded into the API request so the appended
 * page lines up with the server-side ordering of the existing items.
 */
export function useLoadMoreByStatus(
  status: TaskStatus,
  myTasks?: { scope: string; filter: MyTasksFilter },
  sort?: TaskSortParam,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [isLoading, setIsLoading] = useState(false);

  const activeKey = myTasks
    ? taskKeys.myListSorted(wsId, myTasks.scope, myTasks.filter, sort)
    : taskKeys.listSorted(wsId, sort);
  const cache = qc.getQueryData<ListTasksCache>(activeKey);
  const bucket = cache?.byStatus[status];
  const loaded = bucket?.tasks.length ?? 0;
  const total = bucket?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listTasks({
        status,
        limit: TASK_PAGE_SIZE,
        offset: loaded,
        ...sort,
        ...myTasks?.filter,
      });
      qc.setQueryData<ListTasksCache>(activeKey, (old) => {
        if (!old) return old;
        const prev = getBucket(old, status);
        const existingIds = new Set(prev.tasks.map((i) => i.id));
        const appended = res.tasks.filter((i) => !existingIds.has(i.id));
        return setBucket(old, status, {
          tasks: [...prev.tasks, ...appended],
          total: res.total,
        });
      });
    } finally {
      setIsLoading(false);
    }
  }, [qc, activeKey, status, loaded, hasMore, isLoading, myTasks?.filter, sort]);

  return { loadMore, hasMore, isLoading, total };
}

/**
 * Paginate one assignee-grouped board column into the cache. `queryKey`
 * already pins the active cache entry (it's the same object the consuming
 * `useQuery` registered), so the cache lookup and `setQueryData` target the
 * right row. `sort` is threaded into the API request so the appended page
 * lines up with the server-side ordering of the existing items.
 */
export function useLoadMoreByAssigneeGroup(
  group: Pick<TaskAssigneeGroup, "id" | "assignee_type" | "assignee_id">,
  queryKey: QueryKey,
  filter: AssigneeGroupedTasksFilter,
  sort?: TaskSortParam,
) {
  const qc = useQueryClient();
  const [isLoading, setIsLoading] = useState(false);

  const cache = qc.getQueryData<GroupedTasksResponse>(queryKey);
  const cachedGroup = cache?.groups.find((g) => g.id === group.id);
  const loaded = cachedGroup?.tasks.length ?? 0;
  const total = cachedGroup?.total ?? 0;
  const hasMore = loaded < total;

  const loadMore = useCallback(async () => {
    if (isLoading || !hasMore) return;
    setIsLoading(true);
    try {
      const res = await api.listGroupedTasks({
        group_by: "assignee",
        limit: TASK_PAGE_SIZE,
        offset: loaded,
        ...sort,
        ...filter,
        group_assignee_type: group.assignee_type ?? "none",
        group_assignee_id: group.assignee_id ?? undefined,
      });
      const nextGroup = res.groups[0];
      if (!nextGroup) return;

      qc.setQueryData<GroupedTasksResponse>(queryKey, (old) => {
        if (!old) return old;
        return {
          groups: old.groups.map((existing) => {
            if (existing.id !== nextGroup.id) return existing;
            const existingIds = new Set(existing.tasks.map((task) => task.id));
            const appended = nextGroup.tasks.filter((task) => !existingIds.has(task.id));
            return {
              ...existing,
              tasks: [...existing.tasks, ...appended],
              total: nextGroup.total,
            };
          }),
        };
      });
    } finally {
      setIsLoading(false);
    }
  }, [filter, group.assignee_id, group.assignee_type, hasMore, isLoading, loaded, qc, queryKey, sort]);

  return { loadMore, hasMore, isLoading, total };
}

// ---------------------------------------------------------------------------
// Task CRUD
// ---------------------------------------------------------------------------

export function useCreateTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTaskRequest) => api.createTask(data),
    onSuccess: (newTask) => {
      for (const [key, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
        if (data) qc.setQueryData<ListTasksCache>(key, addTaskToBuckets(data, newTask));
      }
      // Workflow boards render the Task snapshot embedded in the selected
      // instance detail. New membership can also reactivate the run, so refresh
      // both the detail and run lists after an SOP-assigned Task is created.
      if (newTask.workflow_instance_id) {
        qc.invalidateQueries({queryKey: workflowKeys.instance(wsId, newTask.workflow_instance_id)});
        qc.invalidateQueries({queryKey: [...workflowKeys.all(wsId), "instances"]});
      }
      // Surface the just-created task in cmd+k's Recent list without
      // requiring the user to open it first.
      useRecentTasksStore.getState().recordVisit(wsId, newTask.id);
      // Invalidate parent's children query so sub-tasks list updates immediately
      if (newTask.parent_task_id) {
        qc.invalidateQueries({ queryKey: taskKeys.children(wsId, newTask.parent_task_id) });
        qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

export function useUpdateTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({ id, move_intent: moveIntent, ...data }: UpdateTaskMutationInput) => {
      const current = qc.getQueryData<Task>(taskKeys.detail(wsId, id));
      if (!moveIntent) {
        try {
          return await api.updateTask(id, {
            ...data,
            ...(current?.revision
              ? { expected_revision: current.revision }
              : {}),
          });
        } catch (error) {
          const conflict = revisionConflictBody(error);
          const latestRevision =
            typeof conflict?.current.revision === "string"
              ? conflict.current.revision
              : undefined;
          if (
            !conflict ||
            !latestRevision ||
            !canRetryTaskRevisionConflict(data)
          ) {
            throw error;
          }
          return api.updateTask(id, {
            ...data,
            expected_revision: latestRevision,
          });
        }
      }
      const { position: _optimisticPosition, ...target } = data;
      return api.moveTask(id, { ...target, ...moveIntent });
    },
    onMutate: ({ id, move_intent: _moveIntent, ...data }) => {
      // suppress_run / handoff_note are write-time control fields, not Task
      // columns — they steer enqueue/injection on the server and must never be
      // written into the query cache (SILI-3375). Strip them from the patch; the
      // mutationFn above still sends the full payload to the API.
      const { suppress_run: _suppressRun, handoff_note: _handoffNote, ...patch } = data;
      // Fire-and-forget cancelQueries — keeps onMutate synchronous so the
      // cache update happens in the same tick as mutate(). Awaiting would
      // yield to the event loop, letting @dnd-kit reset its visual state
      // before the optimistic update lands.
      qc.cancelQueries({ queryKey: taskKeys.list(wsId) });
      qc.cancelQueries({ queryKey: taskKeys.myAll(wsId) });
      qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) });
      qc.cancelQueries({ queryKey: taskKeys.tableAll(wsId) });
      if (patch.status !== undefined) {
        qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      }
      const prevDetail = qc.getQueryData<Task>(taskKeys.detail(wsId, id));
      // The coordinator owns the cross-cache rules: surgical patch/rebucket
      // where the card is loaded and still belongs, surgical REMOVE where the
      // change moves it off a filtered surface, stale-key bookkeeping where
      // the server result may have drifted (invalidated on settle, not here —
      // a mid-flight refetch would stomp the optimistic state).
      const change = applyTaskChange(qc, wsId, id, patch as Partial<Task>, {
        changed: taskChangedDims(patch, prevDetail),
        baseTask: prevDetail,
      });

      // Resolve parent_task_id from the freshest source so we can keep the
      // parent's children cache in sync (used by the parent task's
      // sub-tasks list). Falls back to scanning loaded children caches —
      // when the user navigates straight to a parent's detail page, the
      // child may live only there, not in detail/list.
      let parentId: string | null =
        prevDetail?.parent_task_id ??
        change.prevTask?.parent_task_id ??
        null;
      if (!parentId) {
        const childrenCaches = qc.getQueriesData<Task[]>({
          queryKey: [...taskKeys.all(wsId), "children"],
        });
        for (const [key, data] of childrenCaches) {
          if (!data?.some((c) => c.id === id)) continue;
          const candidate = key[key.length - 1];
          if (typeof candidate === "string") {
            parentId = candidate;
            break;
          }
        }
      }
      const prevChildren = parentId
        ? qc.getQueryData<Task[]>(taskKeys.children(wsId, parentId))
        : undefined;

      if (parentId) {
        // When the write re-parents this task away from `parentId` (detach
        // to standalone, or move under a different parent), prune it from the
        // old parent's children cache. The parent's sub-tasks list renders
        // that array directly, so a bare patch to parent_task_id: null would
        // leave an orphaned row in the list until the settle refetch lands.
        // onError restores prevChildren, so the prune rolls back on failure.
        const detachedFromParent =
          Object.prototype.hasOwnProperty.call(patch, "parent_task_id") &&
          patch.parent_task_id !== parentId;
        qc.setQueryData<Task[]>(
          taskKeys.children(wsId, parentId),
          (old) =>
            detachedFromParent
              ? old?.filter((c) => c.id !== id)
              : old?.map((c) => (c.id === id ? { ...c, ...patch } : c)),
        );
      }
      return { change, prevChildren, parentId, id };
    },
    onError: (err, vars, ctx) => {
      if (ctx) {
        rollbackTaskChange(qc, wsId, ctx.id, ctx.change);
      }
      if (ctx?.parentId && ctx.prevChildren !== undefined) {
        qc.setQueryData(
          taskKeys.children(wsId, ctx.parentId),
          ctx.prevChildren,
        );
      }
      const conflict = revisionConflictBody(err);
      if (conflict && !vars.move_intent) {
        const { id, move_intent: _moveIntent, ...attemptedUpdate } = vars;
        useRevisionConflictStore.getState().open({
          kind: "task",
          current: conflict.current,
          attempted: conflict.attempted,
          retry: async () => {
            // This is the explicit force-overwrite action. Reusing even the
            // server's current revision leaves a race window and makes the
            // button behave like another optimistic retry instead of the
            // override the user selected.
            const { expected_revision: _expectedRevision, ...forcedUpdate } = attemptedUpdate;
            const updated = await api.updateTask(id, forcedUpdate);
            qc.setQueryData(taskKeys.detail(wsId, id), updated);
            return updated;
          },
          refresh: async () => {
            // A Task shown on an SOP Stage board is embedded in the workflow
            // instance snapshot. Refresh both caches so closing/reopening the
            // conflict dialog cannot leave the board (or its Task detail)
            // holding the stale revision that caused the conflict.
            await qc.refetchQueries({
              queryKey: taskKeys.detail(wsId, id),
              type: "active",
            });
            const workflowInstanceId =
              typeof conflict.current.workflow_instance_id === "string"
                ? conflict.current.workflow_instance_id
                : undefined;
            if (workflowInstanceId) {
              await qc.refetchQueries({
                queryKey: workflowKeys.instance(wsId, workflowInstanceId),
                type: "active",
              });
            }
            await qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
          },
        });
      }
    },
    onSuccess: (serverTask, vars) => {
      // Reconcile with the authoritative server entity by patching the one card
      // in place — NOT by invalidating + refetching the list. The list refetch
      // is what made a successful move flicker: the optimistic card was already
      // in the right place, then the refetch replaced the whole column and the
      // card re-landed. updateTask returns the full task and a position update
      // touches only that row, so a surgical patch is the authoritative
      // reconcile and is a visual no-op when the optimistic value matched.
      //
      // baseTask = serverTask: membership moves were already handled
      // optimistically; against the post-write entity the changed dims come
      // out false unless the server coerced a different value, so this pass
      // is the plain surgical patch it always was.
      const {
        suppress_run: _suppressRun,
        handoff_note: _handoffNote,
        move_intent: _moveIntent,
        id: _id,
        ...intent
      } = vars;
      // Drop `properties` from the reconcile payload: the bag is owned by the
      // property mutation pipeline (single-key atomic writes + its own
      // optimistic state). An UpdateTask snapshot taken before a concurrent
      // property write resolves would otherwise overwrite the newer bag
      // (clean-room review F3 response-ordering race).
      const { properties: _staleBag, ...reconcilable } = serverTask;
      const reconcile = applyTaskChange(qc, wsId, serverTask.id, reconcilable as typeof serverTask, {
        changed: taskChangedDims(intent, serverTask),
        baseTask: serverTask,
      });
      // The server has committed — safe to flush any drift it reported now.
      invalidateStaleListKeys(qc, reconcile.staleKeys);
    },
    onSettled: (_data, _err, vars, ctx) => {
      // The task's own list + detail caches are reconciled surgically in
      // onSuccess / onError, so they are deliberately NOT invalidated here — a
      // full-list refetch on settle is what made drags flicker. Only aggregate
      // caches that cannot be patched from a single task are refreshed, plus
      // the specific list keys the coordinator flagged as drifted (unknown
      // membership, enter/leave beyond the loaded window, bucket-count drift).
      // Those stale keys are the surgical replacement for the old blanket
      // "invalidate myAll on project move" safety net (SILI-3669 / #4548): the
      // old project's loaded list already had the card removed in onMutate,
      // and only genuinely undecidable lists refetch here.
      invalidateTaskDerivatives(qc, wsId, {
        statusOrProjectChanged:
          vars.status !== undefined ||
          Object.prototype.hasOwnProperty.call(vars, "project_id"),
      });
      qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.change.staleKeys);
      }
      // Refresh the task's attachments cache when the description editor
      // bound new uploads — the description editor reads `taskAttachments`
      // to resolve text-preview Eye gates, and unlike other mutations this
      // payload mutates the attachment join table.
      if (vars.attachment_ids?.length) {
        qc.invalidateQueries({ queryKey: taskKeys.attachments(vars.id) });
      }
      // Invalidate old parent's children cache
      if (ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: taskKeys.children(wsId, ctx.parentId),
        });
        qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
      }
      // Invalidate new parent's children cache when parent_task_id changed
      const newParentId = vars.parent_task_id;
      if (newParentId && newParentId !== ctx?.parentId) {
        qc.invalidateQueries({
          queryKey: taskKeys.children(wsId, newParentId),
        });
        qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
      }
      // Invalidate the batched-children cache only when the parent link
      // actually changed. The WS path (ws-updaters.ts) invalidates
      // unconditionally because it doesn't know what the server change
      // touched; here onMutate already patched taskKeys.children(parent)
      // optimistically, so we only need to flush when the parent relation
      // itself moved.
      if (ctx?.parentId || newParentId) {
        qc.invalidateQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) });
      }
      // Moving a Task into, out of, or between SOP runs changes the embedded
      // task snapshots on both the previous and resulting instance. Refresh
      // only those concrete runs to keep Stage boards consistent without
      // refetching every Workflow in the Space.
      const affectedWorkflowInstances = new Set(
        [
          ctx?.change.prevTask?.workflow_instance_id,
          _data?.workflow_instance_id,
          vars.workflow_instance_id,
        ].filter((id): id is string => Boolean(id)),
      );
      if (affectedWorkflowInstances.size > 0) {
        qc.invalidateQueries({ queryKey: [...workflowKeys.all(wsId), "instances"] });
      }
      for (const instanceId of affectedWorkflowInstances) {
        qc.invalidateQueries({
          queryKey: workflowKeys.instance(wsId, instanceId),
        });
      }
    },
  });
}

export function useDeleteTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteTask(id),
    onMutate: async (id) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: taskKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: taskKeys.myAll(wsId) }),
        qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) }),
      ]);
      const metadata = collectDeletedTaskCacheMetadata(qc, wsId, id);
      await Promise.all(
        metadata.parentTaskIds.map((parentId) =>
          qc.cancelQueries({ queryKey: taskKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListTasksCache>({
        queryKey: taskKeys.myAll(wsId),
      });
      const prevFlatLists = qc.getQueriesData<TaskFlatCache>({
        queryKey: taskKeys.flatAll(wsId),
      });
      const prevDetail = qc.getQueryData<Task>(taskKeys.detail(wsId, id));
      const prevChildren = new Map<string, Task[] | undefined>();
      for (const parentId of metadata.parentTaskIds) {
        prevChildren.set(
          parentId,
          qc.getQueryData<Task[]>(taskKeys.children(wsId, parentId)),
        );
      }

      pruneDeletedTaskFromListCaches(qc, wsId, id);
      pruneDeletedTaskFromParentChildrenCaches(qc, wsId, id, metadata);
      qc.removeQueries({ queryKey: taskKeys.detail(wsId, id) });
      return {
        id,
        metadata,
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevDetail,
        prevChildren,
      };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetail) {
        qc.setQueryData(taskKeys.detail(wsId, ctx.id), ctx.prevDetail);
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(taskKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (_data, id, ctx) => {
      useRecentContextStore.getState().forgetContext(wsId, { type: "task", id });
      cleanupDeletedTaskCaches(qc, wsId, id, ctx?.metadata);
    },
    onSettled: (_data, _err, _id, ctx) => {
      qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.metadata) invalidateDeletedTaskParentCaches(qc, wsId, ctx.metadata);
    },
  });
}

export function useBatchUpdateTasks() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      ids,
      updates,
    }: {
      ids: string[];
      updates: UpdateTaskRequest;
    }) => api.batchUpdateTasks(ids, updates),
    onMutate: async ({ ids, updates }) => {
      // Control fields steer the server; they are not Task columns and must
      // not enter the cache (SILI-3375). mutationFn still sends them.
      const { suppress_run: _suppressRun, handoff_note: _handoffNote, ...patch } = updates;
      await qc.cancelQueries({ queryKey: taskKeys.list(wsId) });
      await qc.cancelQueries({ queryKey: taskKeys.myAll(wsId) });
      await qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) });
      await qc.cancelQueries({ queryKey: taskKeys.tableAll(wsId) });
      if (patch.status !== undefined) {
        await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      }

      // Run every task through the coordinator — the same rules table the
      // single-task update uses, so a batch edit patches/removes across the
      // workspace board AND every filtered myList surface identically.
      // Snapshots are first-wins per cache key: after the first task's
      // application a cache already carries partial patches, so only the
      // first snapshot per key is pristine for rollback.
      const prevListByHash = new Map<string, [QueryKey, ListTasksCache]>();
      const prevFlatListByHash = new Map<string, [QueryKey, TaskFlatCache]>();
      const prevTableRowByHash = new Map<
        string,
        [QueryKey, TaskTableRowCache]
      >();
      const prevDetailById = new Map<string, Task>();
      let prevInboxList: InboxItem[] | undefined;
      const staleKeys: QueryKey[] = [];
      for (const id of ids) {
        const base = qc.getQueryData<Task>(taskKeys.detail(wsId, id));
        const change = applyTaskChange(qc, wsId, id, patch as Partial<Task>, {
          changed: taskChangedDims(patch, base),
          baseTask: base,
        });
        for (const [key, snapshot] of change.prevLists) {
          const hash = hashKey(key);
          if (!prevListByHash.has(hash)) prevListByHash.set(hash, [key, snapshot]);
        }
        for (const [key, snapshot] of change.prevFlatLists) {
          const hash = hashKey(key);
          if (!prevFlatListByHash.has(hash)) {
            prevFlatListByHash.set(hash, [key, snapshot]);
          }
        }
        for (const [key, snapshot] of change.prevTableRows) {
          const hash = hashKey(key);
          if (!prevTableRowByHash.has(hash)) {
            prevTableRowByHash.set(hash, [key, snapshot]);
          }
        }
        if (change.prevDetail) prevDetailById.set(id, change.prevDetail);
        if (prevInboxList === undefined && change.prevInboxList !== undefined) {
          prevInboxList = change.prevInboxList;
        }
        staleKeys.push(...change.staleKeys);
      }

      // Mirror the optimistic patch into any loaded children cache so
      // sub-task rows on a parent's detail page reflect the change too.
      const idSet = new Set(ids);
      const childrenCaches = qc.getQueriesData<Task[]>({
        queryKey: [...taskKeys.all(wsId), "children"],
      });
      const prevChildren = new Map<string, Task[] | undefined>();
      const affectedParentIds = new Set<string>();
      for (const [key, data] of childrenCaches) {
        if (!data?.some((c) => idSet.has(c.id))) continue;
        const parentId = key[key.length - 1];
        if (typeof parentId !== "string") continue;
        affectedParentIds.add(parentId);
        prevChildren.set(parentId, data);
        qc.setQueryData<Task[]>(taskKeys.children(wsId, parentId), (old) =>
          old?.map((c) => (idSet.has(c.id) ? { ...c, ...patch } : c)),
        );
      }

      return {
        prevLists: [...prevListByHash.values()],
        prevFlatLists: [...prevFlatListByHash.values()],
        prevTableRows: [...prevTableRowByHash.values()],
        prevDetailById,
        prevInboxList,
        staleKeys,
        prevChildren,
        affectedParentIds,
      };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevTableRows) {
        for (const [key, snapshot] of ctx.prevTableRows) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevDetailById) {
        for (const [id, snapshot] of ctx.prevDetailById) {
          qc.setQueryData(taskKeys.detail(wsId, id), snapshot);
        }
      }
      if (ctx?.prevInboxList !== undefined) {
        qc.setQueryData(inboxKeys.list(wsId), ctx.prevInboxList);
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(taskKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSettled: (_data, _err, _vars, ctx) => {
      // Deliberately NOT invalidating taskKeys.list / myAll here: the onMutate
      // pass above is a complete surgical reconcile for the loaded bucketed
      // boards, so a full-board refetch on settle would only re-introduce the
      // flicker the single-task update already removed. Aggregate / grouped
      // caches that cannot be recomputed from a single-task patch are
      // refreshed below, plus the specific keys the coordinator flagged as
      // drifted — the surgical replacement for the old blanket "invalidate
      // myAll on project move" safety net (SILI-3669 / #4548).
      invalidateTaskDerivatives(qc, wsId, {
        statusOrProjectChanged:
          _vars.updates.status !== undefined ||
          Object.prototype.hasOwnProperty.call(_vars.updates, "project_id"),
      });
      qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
      if (ctx) {
        invalidateStaleListKeys(qc, ctx.staleKeys);
      }
      if (ctx?.affectedParentIds && ctx.affectedParentIds.size > 0) {
        for (const parentId of ctx.affectedParentIds) {
          qc.invalidateQueries({
            queryKey: taskKeys.children(wsId, parentId),
          });
        }
        qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
      }
    },
  });
}

export function useBatchDeleteTasks() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (ids: string[]) => api.batchDeleteTasks(ids),
    onMutate: async (ids) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: taskKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: taskKeys.myAll(wsId) }),
        qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) }),
      ]);
      const metadataById = new Map(
        ids.map((id) => [
          id,
          collectDeletedTaskCacheMetadata(qc, wsId, id),
        ]),
      );
      const parentTaskIds = new Set<string>();
      for (const metadata of metadataById.values()) {
        for (const parentId of metadata.parentTaskIds) {
          parentTaskIds.add(parentId);
        }
      }
      await Promise.all(
        Array.from(parentTaskIds).map((parentId) =>
          qc.cancelQueries({ queryKey: taskKeys.children(wsId, parentId) }),
        ),
      );
      const prevLists = qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) });
      const prevMyLists = qc.getQueriesData<ListTasksCache>({
        queryKey: taskKeys.myAll(wsId),
      });
      const prevFlatLists = qc.getQueriesData<TaskFlatCache>({
        queryKey: taskKeys.flatAll(wsId),
      });
      const prevChildren = new Map<string, Task[] | undefined>();
      for (const parentId of parentTaskIds) {
        prevChildren.set(
          parentId,
          qc.getQueryData<Task[]>(taskKeys.children(wsId, parentId)),
        );
      }

      for (const id of ids) {
        const metadata = metadataById.get(id);
        pruneDeletedTaskFromListCaches(qc, wsId, id);
        if (metadata) {
          pruneDeletedTaskFromParentChildrenCaches(qc, wsId, id, metadata);
        }
      }
      return {
        prevLists,
        prevMyLists,
        prevFlatLists,
        prevChildren,
        parentTaskIds,
        metadataById,
      };
    },
    onError: (_err, _ids, ctx) => {
      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(taskKeys.children(wsId, parentId), snapshot);
        }
      }
    },
    onSuccess: (data, ids, ctx) => {
      if (data.deleted === ids.length) {
        const { forgetContext } = useRecentContextStore.getState();
        for (const id of ids) {
          forgetContext(wsId, { type: "task", id });
          cleanupDeletedTaskCaches(qc, wsId, id, ctx?.metadataById.get(id));
        }
        return;
      }

      if (ctx?.prevLists) {
        for (const [key, snapshot] of ctx.prevLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevMyLists) {
        for (const [key, snapshot] of ctx.prevMyLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevFlatLists) {
        for (const [key, snapshot] of ctx.prevFlatLists) {
          qc.setQueryData(key, snapshot);
        }
      }
      if (ctx?.prevChildren) {
        for (const [parentId, snapshot] of ctx.prevChildren) {
          qc.setQueryData(taskKeys.children(wsId, parentId), snapshot);
        }
      }
      for (const id of ids) {
        invalidateTaskScopedCaches(qc, wsId, id);
      }
      qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
      invalidateDeletedTaskDependentCaches(qc, wsId);
    },
    onSettled: (_data, _err, _ids, ctx) => {
      qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.flatAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
      if (ctx?.parentTaskIds && ctx.parentTaskIds.size > 0) {
        invalidateDeletedTaskParentCaches(qc, wsId, {
          parentTaskIds: Array.from(ctx.parentTaskIds),
        });
      }
    },
  });
}

// ---------------------------------------------------------------------------
// Comments / Timeline
// ---------------------------------------------------------------------------

type TimelineCache = TimelineEntry[];

export function useCreateComment(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      content,
      type,
      parentId,
      attachmentIds,
      suppressAgentIds,
    }: {
      content: string;
      type?: string;
      parentId?: string;
      attachmentIds?: string[];
      suppressAgentIds?: string[];
    }) => api.createComment(taskId, content, type, parentId, attachmentIds, suppressAgentIds),
    onSuccess: (comment) => {
      const entry: TimelineEntry = {
        type: "comment",
        id: comment.id,
        actor_type: comment.author_type,
        actor_id: comment.author_id,
        content: comment.content,
        parent_id: comment.parent_id,
        comment_type: comment.type,
        reactions: comment.reactions ?? [],
        attachments: comment.attachments ?? [],
        created_at: comment.created_at,
        updated_at: comment.updated_at,
      };
      // Dedupe by id: the `comment:created` WS event may have already added
      // this entry from the broadcast path before this onSuccess fires. Skip
      // the append if the entry is already in the cache.
      qc.setQueryData<TimelineCache>(taskKeys.timeline(taskId), (old) => {
        if (!old) return [entry];
        if (old.some((e) => e.id === entry.id)) return old;
        return sortTimelineEntriesAsc([...old, entry]);
      });
      // Posting a comment changes the trigger answer itself (the enqueued
      // task now dedupes follow-up triggers), so cached previews for this
      // task are stale the moment the create lands.
      qc.invalidateQueries({ queryKey: taskKeys.commentTriggerPreview(taskId) });
    },
    // No onSettled invalidate. The `comment:created` WS broadcast keeps
    // the timeline cache fresh after a successful create, and reconnect
    // recovery in useTaskTimeline already invalidates if the connection
    // dropped. Re-fetching on every submit replaces every entry's
    // reference, which forces every memoized CommentCard subtree to
    // re-render (visible as a flash across sibling threads during AI
    // streaming).
  });
}

export function useUpdateComment(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
      suppressAgentIds,
    }: {
      commentId: string;
      content: string;
      attachmentIds: string[];
      suppressAgentIds?: string[];
    }) => {
      const current = qc.getQueryData<TimelineCache>(taskKeys.timeline(taskId))?.find((entry) => entry.id === commentId);
      const expectedRevision = current && "revision" in current && typeof current.revision === "string" ? current.revision : undefined;
      return api.updateComment(commentId, content, attachmentIds, suppressAgentIds, expectedRevision);
    },
    onMutate: async ({ commentId, content, attachmentIds }) => {
      await qc.cancelQueries({ queryKey: taskKeys.timeline(taskId) });
      const prev = qc.getQueryData<TimelineCache>(taskKeys.timeline(taskId));
      const kept = new Set(attachmentIds);
      qc.setQueryData<TimelineCache>(taskKeys.timeline(taskId), (old) =>
        old?.map((e) =>
          e.id === commentId
            ? { ...e, content, attachments: e.attachments?.filter((a) => kept.has(a.id)) }
            : e,
        ),
      );
      return { prev };
    },
    onError: (err, vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(taskKeys.timeline(taskId), ctx.prev);
      }
      const conflict = revisionConflictBody(err);
      if (conflict) {
        const revision = typeof conflict.current.revision === "string" ? conflict.current.revision : undefined;
        useRevisionConflictStore.getState().open({
          kind: "comment",
          current: conflict.current,
          attempted: conflict.attempted,
          retry: () => api.updateComment(vars.commentId, vars.content, vars.attachmentIds, vars.suppressAgentIds, revision),
          refresh: () => qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) }),
        });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) });
    },
  });
}

export function useDeleteComment(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (commentId: string) => api.deleteComment(commentId),
    onMutate: async (commentId) => {
      await qc.cancelQueries({ queryKey: taskKeys.timeline(taskId) });
      const prev = qc.getQueryData<TimelineCache>(taskKeys.timeline(taskId));

      // Cascade: collect all descendants of the deleted comment.
      const toRemove = new Set<string>([commentId]);
      if (prev) {
        let changed = true;
        while (changed) {
          changed = false;
          for (const e of prev) {
            if (
              e.parent_id &&
              toRemove.has(e.parent_id) &&
              !toRemove.has(e.id)
            ) {
              toRemove.add(e.id);
              changed = true;
            }
          }
        }
      }

      qc.setQueryData<TimelineCache>(taskKeys.timeline(taskId), (old) =>
        old?.filter((e) => !toRemove.has(e.id)),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(taskKeys.timeline(taskId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) });
    },
  });
}

// Every comment id in the same thread as `commentId` — the thread root plus
// every descendant. Mirrors the server's thread walk in
// ClearOtherThreadResolutions so the resolve optimistic update can clear sibling
// resolutions exactly as the backend will, instead of briefly showing two
// resolutions until the refetch settles.
function collectThreadCommentIds(
  entries: TimelineCache,
  commentId: string,
): Set<string> {
  const byId = new Map<string, TimelineEntry>();
  for (const e of entries) {
    if (e.type === "comment") byId.set(e.id, e);
  }
  // Walk up to the thread root (cycle-guarded against malformed parent chains).
  let rootId = commentId;
  const guard = new Set<string>();
  let cur = byId.get(commentId);
  while (cur?.parent_id && byId.has(cur.parent_id) && !guard.has(cur.id)) {
    guard.add(cur.id);
    rootId = cur.parent_id;
    cur = byId.get(cur.parent_id);
  }
  // Expand back down over the whole subtree.
  const childrenByParent = new Map<string, string[]>();
  for (const e of byId.values()) {
    if (e.parent_id) {
      const list = childrenByParent.get(e.parent_id) ?? [];
      list.push(e.id);
      childrenByParent.set(e.parent_id, list);
    }
  }
  const ids = new Set<string>([rootId]);
  const stack = [rootId];
  while (stack.length > 0) {
    const id = stack.pop()!;
    for (const child of childrenByParent.get(id) ?? []) {
      if (!ids.has(child)) {
        ids.add(child);
        stack.push(child);
      }
    }
  }
  return ids;
}

export function useResolveComment(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ commentId, resolved }: { commentId: string; resolved: boolean }) =>
      resolved ? api.resolveComment(commentId) : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      await qc.cancelQueries({ queryKey: taskKeys.timeline(taskId) });
      const prev = qc.getQueryData<TimelineCache>(taskKeys.timeline(taskId));
      qc.setQueryData<TimelineCache>(taskKeys.timeline(taskId), (old) => {
        if (!old) return old;
        // Resolving makes this comment the sole resolution in its thread, so
        // mirror the server (ClearOtherThreadResolutions) and clear every other
        // resolution in the same thread. Without this the cache shows two
        // resolutions until the settle refetch, which is exactly the flash the
        // single-resolution fix removes. Unresolve only clears its own row.
        const threadIds = resolved ? collectThreadCommentIds(old, commentId) : null;
        return old.map((e) => {
          if (e.id === commentId) {
            return {
              ...e,
              resolved_at: resolved ? new Date().toISOString() : null,
              resolved_by_type: resolved ? e.resolved_by_type ?? null : null,
              resolved_by_id: resolved ? e.resolved_by_id ?? null : null,
            };
          }
          if (resolved && e.resolved_at && threadIds?.has(e.id)) {
            return { ...e, resolved_at: null, resolved_by_type: null, resolved_by_id: null };
          }
          return e;
        });
      });
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) {
        qc.setQueryData(taskKeys.timeline(taskId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) });
    },
  });
}

export function useToggleCommentReaction(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["toggleCommentReaction", taskId] as const,
    mutationFn: async ({
      commentId,
      emoji,
      existing,
    }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(taskId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Task-level Reactions
// ---------------------------------------------------------------------------

export function useToggleTaskReaction(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["toggleTaskReaction", taskId] as const,
    mutationFn: async ({
      emoji,
      existing,
    }: ToggleTaskReactionVars) => {
      if (existing) {
        await api.removeTaskReaction(taskId, emoji);
        return null;
      }
      return api.addTaskReaction(taskId, emoji);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.reactions(taskId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Task Subscribers
// ---------------------------------------------------------------------------

export function useToggleTaskSubscriber(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({
      userId,
      userType,
      subscribed,
    }: {
      userId: string;
      userType: "member" | "agent";
      subscribed: boolean;
    }) => {
      if (subscribed) {
        await api.unsubscribeFromTask(taskId, userId, userType);
      } else {
        await api.subscribeToTask(taskId, userId, userType);
      }
    },
    onMutate: async ({ userId, userType, subscribed }) => {
      await qc.cancelQueries({ queryKey: taskKeys.subscribers(taskId) });
      const prev = qc.getQueryData<TaskSubscriber[]>(
        taskKeys.subscribers(taskId),
      );

      if (subscribed) {
        qc.setQueryData<TaskSubscriber[]>(
          taskKeys.subscribers(taskId),
          (old) =>
            old?.filter(
              (s) => !(s.user_id === userId && s.user_type === userType),
            ),
        );
      } else {
        const temp: TaskSubscriber = {
          task_id: taskId,
          user_type: userType,
          user_id: userId,
          reason: "manual",
          created_at: new Date().toISOString(),
        };
        qc.setQueryData<TaskSubscriber[]>(
          taskKeys.subscribers(taskId),
          (old) => {
            if (
              old?.some(
                (s) => s.user_id === userId && s.user_type === userType,
              )
            )
              return old;
            return [...(old ?? []), temp];
          },
        );
      }
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev)
        qc.setQueryData(taskKeys.subscribers(taskId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.subscribers(taskId) });
    },
  });
}
