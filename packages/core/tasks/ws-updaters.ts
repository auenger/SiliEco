import type { QueryClient } from "@tanstack/react-query";
import { taskKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import { projectKeys } from "../projects/queries";
import {
  applyTaskChange,
  invalidateTaskDerivatives,
  invalidateStaleListKeys,
  invalidateUpdatedAtSortedTaskLists,
  type TaskFlatCache,
} from "./cache-coordinator";
import {
  addTaskToBuckets,
  findTaskLocation,
  patchTaskInBuckets,
} from "./cache-helpers";
import { cleanupDeletedTaskCaches } from "./delete-cache";
import type {
  Task,
  TaskLabelsResponse,
  TaskMetadata,
  TaskPropertyValues,
  TaskTableRowsResponse,
  Label,
} from "../types";
import type { ListTasksCache } from "../types";

function patchTaskInFlatCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  patch: Partial<Task>,
) {
  for (const [key, data] of qc.getQueriesData<TaskFlatCache>({
    queryKey: taskKeys.flatAll(wsId),
  })) {
    if (!data?.pages) continue;
    qc.setQueryData<TaskFlatCache>(key, {
      ...data,
      pages: data.pages.map((page) => ({
        ...page,
        tasks: page.tasks.map((task) =>
          task.id === taskId ? { ...task, ...patch } : task,
        ),
      })),
    });
  }
}

/** Patch denormalized task snapshots in every loaded per-parent cache. */
function patchTaskInChildrenCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  patch: Partial<Task>,
) {
  for (const [key, data] of qc.getQueriesData<Task[]>({
    queryKey: taskKeys.childrenAll(wsId),
  })) {
    if (!data || !data.some((child) => child.id === taskId)) continue;
    qc.setQueryData<Task[]>(
      key,
      data.map((child) =>
        child.id === taskId ? { ...child, ...patch } : child,
      ),
    );
  }
}

function patchTaskInTableCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  patch: Partial<Task>,
) {
  for (const [key, data] of qc.getQueriesData<unknown>({
    queryKey: taskKeys.tableAll(wsId),
  })) {
    if (
      !data ||
      typeof data !== "object" ||
      !Array.isArray((data as TaskTableRowsResponse).rows)
    ) {
      continue;
    }
    const page = data as TaskTableRowsResponse;
    if (!page.rows.some((row) => row.task.id === taskId)) continue;
    qc.setQueryData<TaskTableRowsResponse>(key, {
      ...page,
      rows: page.rows.map((row) =>
        row.task.id === taskId
          ? { ...row, task: { ...row.task, ...patch } }
          : row,
      ),
    });
  }
}

function findTaskInFlatCaches(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  for (const [, data] of qc.getQueriesData<TaskFlatCache>({
    queryKey: taskKeys.flatAll(wsId),
  })) {
    for (const page of data?.pages ?? []) {
      const task = page.tasks.find((candidate) => candidate.id === taskId);
      if (task) return task;
    }
  }
  return undefined;
}

export function onTaskCreated(
  qc: QueryClient,
  wsId: string,
  task: Task,
) {
  for (const [key, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListTasksCache>(key, addTaskToBuckets(data, task));
  }
  qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.flatAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
  if (task.project_id) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
  // Refresh every Project Gantt cache that might be observing this task.
  // We invalidate the whole prefix rather than the task's own project
  // because a fresh task isn't necessarily scheduled yet; the active Gantt
  // page (if any) will refetch and pick it up if it qualifies.
  qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
  if (task.parent_task_id) {
    qc.invalidateQueries({ queryKey: taskKeys.children(wsId, task.parent_task_id) });
    qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
  }
}

export function onTaskUpdated(
  qc: QueryClient,
  wsId: string,
  task: Partial<Task> & { id: string },
  // assigneeChanged / statusChanged / projectChanged come from the server's
  // task:updated flags — authoritative "did this write move a membership
  // dimension" signals. They feed the coordinator's changed-dims input so a
  // non-membership change (title / position / priority / label) keeps every
  // loaded list in place instead of refetching.
  meta: {
    assigneeChanged?: boolean;
    statusChanged?: boolean;
    projectChanged?: boolean;
  } = {},
) {
  // Look up the OLD parent + cached entity before mutating cache state, so we
  // can keep the parent's children cache in sync (powers the sub-tasks list
  // shown on the parent task page) and diff-fallback the change flags.
  const listQueries = qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) });
  const firstListData = listQueries[0]?.[1];
  const detailData = qc.getQueryData<Task>(taskKeys.detail(wsId, task.id));
  const cachedTask =
    detailData ??
    (firstListData ? findTaskLocation(firstListData, task.id)?.task : undefined) ??
    findTaskInFlatCaches(qc, wsId, task.id);
  const oldParentId =
    detailData?.parent_task_id ?? cachedTask?.parent_task_id ?? null;
  // The NEW parent comes from the WS payload when parent_task_id changed
  const newParentId = task.parent_task_id ?? null;
  const parentChanged =
    task.parent_task_id !== undefined && newParentId !== oldParentId;

  // Prefer the server's flags (authoritative, set on the wire). Fall back to
  // diffing the payload against the cached copy only when a flag is absent
  // (older backend): the diff is unreliable once a local optimistic move has
  // overwritten the cached value, but it still covers remote/agent changes
  // and keeps a new frontend on an old backend from regressing (SILI-3669 /
  // #4548). The local move itself is covered by useUpdateTask's own
  // coordinator pass, which never depends on these flags.
  const oldProjectId = detailData?.project_id ?? cachedTask?.project_id ?? null;
  const changed = {
    assignee:
      meta.assigneeChanged ??
      (cachedTask !== undefined &&
        ((task.assignee_id !== undefined &&
          task.assignee_id !== cachedTask.assignee_id) ||
          (task.assignee_type !== undefined &&
            task.assignee_type !== cachedTask.assignee_type))),
    project:
      meta.projectChanged ??
      (task.project_id !== undefined && (task.project_id ?? null) !== oldProjectId),
    status:
      meta.statusChanged ??
      (cachedTask !== undefined &&
        task.status !== undefined &&
        task.status !== cachedTask.status),
  };

  // The coordinator applies the same rules table the local mutations use:
  // surgical patch/rebucket where the card is loaded and still belongs,
  // surgical remove where the change moved it off a filtered surface, and
  // stale keys for the drift a patch cannot fix (enter/leave beyond the
  // loaded window, undecidable membership, off-screen bucket counts). The
  // server has already committed, so stale keys are flushed immediately.
  const change = applyTaskChange(qc, wsId, task.id, task, {
    changed,
    baseTask: cachedTask,
  });
  invalidateStaleListKeys(qc, change.staleKeys);
  invalidateTaskDerivatives(qc, wsId, {
    statusOrProjectChanged:
      task.status !== undefined || task.project_id !== undefined,
  });
  // Group counts, branch membership and hierarchy are server-owned. Never
  // guess deltas from a partial branch; refetch the active Table queries.
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });

  // Invalidate old parent's children (task was removed from it)
  if (oldParentId) {
    if (parentChanged) {
      qc.invalidateQueries({ queryKey: taskKeys.children(wsId, oldParentId) });
    } else {
      qc.setQueryData<Task[]>(taskKeys.children(wsId, oldParentId), (old) =>
        old?.map((c) => (c.id === task.id ? { ...c, ...task } : c)),
      );
    }
  }
  // Invalidate new parent's children (task was added to it)
  if (newParentId && parentChanged) {
    qc.invalidateQueries({ queryKey: taskKeys.children(wsId, newParentId) });
  }
  if (oldParentId || newParentId) {
    if (task.status !== undefined || task.parent_task_id !== undefined) {
      qc.invalidateQueries({ queryKey: taskKeys.childProgress(wsId) });
    }
    qc.invalidateQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) });
  }
}

/**
 * Patch an task's labels in-place across the list cache, my-tasks caches,
 * the detail cache, and the per-task label cache. Triggered by the
 * `task_labels:changed` WS event after attach/detach so list/board chips
 * and the task-detail Properties LabelPicker update without a refetch.
 *
 * The byTask cache backs `LabelPicker`; without patching it, externally
 * driven label changes (agents, other tabs) leave the picker stale until it
 * remounts — `staleTime: Infinity` + `refetchOnWindowFocus: false` (see
 * `query-client.ts`) means focus changes won't recover it.
 */
export function onTaskLabelsChanged(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  labels: Label[],
) {
  patchTaskLabels(qc, wsId, taskId, labels);
  invalidateTaskLabelDerivatives(qc, wsId);
}

/** Deterministic label snapshot patch used by optimistic mutation legs. */
export function patchTaskLabels(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  labels: Label[],
) {
  for (const [key, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListTasksCache>(key, patchTaskInBuckets(data, taskId, { labels }));
  }
  patchTaskInFlatCaches(qc, wsId, taskId, { labels });
  patchTaskInTableCaches(qc, wsId, taskId, { labels });
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId), (old) =>
    old ? { ...old, labels } : old,
  );
  // The sub-tasks panel renders label chips from these denormalized rows.
  patchTaskInChildrenCaches(qc, wsId, taskId, { labels });
  // Patch the Project Gantt caches in-place: the Gantt view applies
  // `labelFilters` to the row data, so a stale `labels` array would silently
  // hide or surface bars after another tab/agent attached or detached a
  // label. Mutating in place (instead of invalidating) avoids a refetch of
  // the entire scheduled set on every label toggle.
  for (const [key, data] of qc.getQueriesData<Task[]>({
    queryKey: taskKeys.projectGanttAll(wsId),
  })) {
    if (!data) continue;
    const next = data.map((task) =>
      task.id === taskId ? { ...task, labels } : task,
    );
    qc.setQueryData<Task[]>(key, next);
  }
}

/** Reconcile server-filtered label windows only after the write commits. */
export function invalidateTaskLabelDerivatives(qc: QueryClient, wsId: string) {
  // A committed response/event must cancel or supersede any per-parent fetch
  // that started before the label write and could otherwise land afterward.
  qc.invalidateQueries({ queryKey: taskKeys.childrenAll(wsId) });
  // Batched children caches hold Map-shaped data (parentId → Task[]) that
  // patchTaskLabels can't surgically update — refetch instead so swimlane
  // child lanes pick up the new label set.
  qc.invalidateQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
  qc.invalidateQueries({
    queryKey: taskKeys.flatAll(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== "object" || Array.isArray(part)) return false;
        const labelIds = (part as { label_ids?: unknown }).label_ids;
        return Array.isArray(labelIds) && labelIds.length > 0;
      }),
  });
}

/**
 * Apply a metadata snapshot to the task detail + list + my-tasks caches.
 * The server emits this whenever a single key is set or deleted, so the
 * payload is always the FULL post-mutation map — we replace, not merge.
 *
 * Used for the read-only metadata strip in task detail. Updates that arrive
 * while no view is mounted still keep the caches accurate so the next render
 * shows the latest state without a refetch.
 */
export function onTaskMetadataChanged(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  metadata: TaskMetadata,
) {
  for (const [key, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListTasksCache>(key, patchTaskInBuckets(data, taskId, { metadata }));
  }
  patchTaskInFlatCaches(qc, wsId, taskId, { metadata });
  patchTaskInTableCaches(qc, wsId, taskId, { metadata });
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) =>
    old ? { ...old, metadata } : old,
  );
  qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
  // A metadata write bumps task.updated_at server-side (SetTaskMetadataKey /
  // DeleteTaskMetadataKey), but the patches above keep each card's slot, so a
  // board/table sorted by "Updated date" would stay in the old order. This
  // event is server-committed, so refetch those keys to re-sort (SILI-5016).
  invalidateUpdatedAtSortedTaskLists(qc, wsId);
  // Server-backed Table counts, membership and cursor boundaries may also
  // depend on metadata-driven timestamps, so refresh its query graph too.
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
}

/**
 * Apply a custom-property bag snapshot to the task detail + list caches.
 * Mirrors onTaskMetadataChanged: the server emits the FULL post-mutation
 * bag on every single-key write, so we replace rather than merge. Also used
 * directly by the useSetTaskProperty/useUnsetTaskProperty optimistic path.
 */
export function onTaskPropertiesChanged(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  properties: TaskPropertyValues,
) {
  patchTaskProperties(qc, wsId, taskId, properties);
  // Per-parent rows are patched for immediate UI feedback, then all children
  // projections are marked stale so older fetches cannot win after commit.
  qc.invalidateQueries({ queryKey: taskKeys.childrenAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
  // Plain assignee-group caches are never patched in place (their bucket
  // shape differs) and would otherwise hold stale chips forever under
  // staleTime:Infinity (clean-room review F2).
  qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.tableAll(wsId) });
  invalidatePropertyWindowQueries(qc, wsId);
  // A property write also bumps task.updated_at server-side
  // (SetTaskPropertyValue / DeleteTaskPropertyValue). invalidatePropertyWindow
  // Queries only refetches property-filtered/-sorted windows, so a status board
  // or flat table sorted by "Updated date" (no property param) would keep the
  // old order. Refetch those too. Only committed callers reach here (WS event +
  // mutation onSuccess); the optimistic leg uses patchTaskProperties (SILI-5016).
  invalidateUpdatedAtSortedTaskLists(qc, wsId);
}

/** Patch only deterministic entity snapshots. Optimistic mutation legs use
 * this helper so they never start a property-filter/sort refetch before the
 * server commit (which could return the old bag and stomp the optimistic one). */
export function patchTaskProperties(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  properties: TaskPropertyValues,
) {
  for (const [key, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
    if (data) qc.setQueryData<ListTasksCache>(key, patchTaskInBuckets(data, taskId, { properties }));
  }
  patchTaskInFlatCaches(qc, wsId, taskId, { properties });
  patchTaskInTableCaches(qc, wsId, taskId, { properties });
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) =>
    old ? { ...old, properties } : old,
  );
  patchTaskInChildrenCaches(qc, wsId, taskId, { properties });
}

/**
 * Refetch every task window whose SERVER-side shape depends on property
 * values: queries filtered by `properties` or sorted by `property:<id>`.
 * In-place patching keeps them stale under staleTime:Infinity — a value
 * edit can change an task's page membership and ordering, and grouped
 * caches never self-heal (review round 3). Windows without property params
 * keep the cheap in-place patch above.
 */
export function invalidatePropertyWindowQueries(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({
    queryKey: taskKeys.all(wsId),
    predicate: (query) =>
      query.queryKey.some((part) => {
        if (!part || typeof part !== "object" || Array.isArray(part)) return false;
        const rec = part as Record<string, unknown>;
        if (
          rec.properties &&
          typeof rec.properties === "object" &&
          Object.keys(rec.properties as Record<string, unknown>).length > 0
        ) {
          return true;
        }
        return typeof rec.sort_by === "string" && rec.sort_by.startsWith("property:");
      }),
  });
}

export function onTaskDeleted(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  cleanupDeletedTaskCaches(qc, wsId, taskId);
  qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
}
