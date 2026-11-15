import {
  hashKey,
  type InfiniteData,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import {
  taskKeys,
  type TaskFlatFilter,
  type TaskSortParam,
  type MyTasksFilter,
} from "./queries";
import { inboxKeys } from "../inbox/queries";
import { patchInboxTaskStatus } from "../inbox/ws-updaters";
import { projectKeys } from "../projects/queries";
import {
  decrementBucketTotal,
  findTaskLocation,
  moveBucketTotal,
  patchTaskInBuckets,
  removeTaskFromBuckets,
} from "./cache-helpers";
import {
  taskMatchesListFilter,
  listFilterDependsOn,
  type TaskChangedDims,
} from "./surface/membership";
import type {
  InboxItem,
  Task,
  TaskTableRowsResponse,
  ListTasksCache,
  ListTasksResponse,
} from "../types";

export type TaskFlatCache = InfiniteData<ListTasksResponse, number>;
export type TaskTableRowCache = TaskTableRowsResponse;

/**
 * TaskCacheCoordinator — the one rules table for how a single task change
 * propagates through the query cache.
 *
 * Every write path converges here: `useUpdateTask` (onMutate optimistic +
 * onSuccess server reconcile), `useBatchUpdateTasks`, and the WS
 * `task:updated` handler all call {@link applyTaskChange}, so "I changed
 * it" and "someone else changed it" follow the same rules by construction.
 *
 * The rules, per loaded bucketed list (workspace board + every myList
 * scope — My Tasks, Project, actor panels, workspace members/agents tabs):
 *
 *   card present, filter untouched by the change → surgical patch (rebucket
 *     on status, position-slot insert; never a refetch — refetching the
 *     visible list is what made drags flicker)
 *   card present, no longer matches the list's filter → surgical REMOVE
 *     (bucket total decremented) — the "task left this surface" case that a
 *     filter-blind patch used to leave behind (SILI-3669)
 *   card present, membership undecidable client-side (involves / my:all) →
 *     patch + mark the key stale
 *   card absent, change can't affect this list → skip
 *   card absent, stayed a member + status changed → move one unit of the
 *     server total between the two buckets immediately, then mark the key
 *     stale: the row's slot in the destination bucket's loaded window is
 *     server knowledge, and under staleTime: Infinity an explicit
 *     invalidation is the only channel that ever fills it in
 *   card absent, left the list (reassigned / re-projected) → old status
 *     bucket total -1
 *   card absent, may have ENTERED, or anything undecidable (no base entity,
 *     unknown membership) → mark the key stale (never hard-insert: the
 *     right page/slot under the list's sort+filter is server knowledge)
 *
 * Stale keys are NOT invalidated here — timing is the caller's contract:
 * mutations defer them to onSettled (invalidating mid-flight would refetch
 * uncommitted state and stomp the optimistic patch), the WS path invalidates
 * immediately (the server already committed).
 *
 * The detail cache and the Inbox `task_status` projection are patched in the
 * same pass. Aggregate projections that cannot be recomputed from one entity
 * (assignee-grouped boards, Gantt, project metrics) go through
 * {@link invalidateTaskDerivatives}.
 */

export interface TaskCacheChangeResult {
  /** Pre-change snapshots of every cache this change touched — feed back to
   *  {@link rollbackTaskChange} from onError. */
  prevLists: [QueryKey, ListTasksCache][];
  prevFlatLists: [QueryKey, TaskFlatCache][];
  prevTableRows: [QueryKey, TaskTableRowCache][];
  prevDetail: Task | undefined;
  prevInboxList: InboxItem[] | undefined;
  /** Loaded list keys whose server result may have drifted (membership
   *  unknown, possible enter/leave beyond the loaded window, bucket-count
   *  drift). Invalidate on settle (mutation) or immediately (WS). */
  staleKeys: QueryKey[];
  /** The freshest pre-change copy of the task found while reconciling —
   *  callers use it for parent/children bookkeeping without re-scanning. */
  prevTask: Task | undefined;
}

/** The server contract a bucketed list key encodes. `myListSorted` keys are
 *  `["tasks", wsId, "my", scope, filter, sort]`; the workspace list is
 *  `["tasks", wsId, "list", sort]` and carries no filter. The `byStatus`
 *  shape check upstream keeps grouped/flat caches under the same prefixes out
 *  of this path. */
function listContractFromKey(key: QueryKey): {
  scope: string | undefined;
  filter: MyTasksFilter;
  sort: TaskSortParam;
} {
  if (key[2] === "my") {
    return {
      scope: typeof key[3] === "string" ? key[3] : undefined,
      filter: (key[4] ?? {}) as MyTasksFilter,
      sort: (key[5] ?? {}) as TaskSortParam,
    };
  }
  return {
    scope: undefined,
    filter: {},
    sort: (key[3] ?? {}) as TaskSortParam,
  };
}

function bucketedListEntries(
  qc: QueryClient,
  wsId: string,
): [QueryKey, ListTasksCache][] {
  return [
    ...qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) }),
    ...qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.myAll(wsId) }),
  ].filter(
    (entry): entry is [QueryKey, ListTasksCache] => !!entry[1]?.byStatus,
  );
}

function flatListEntries(
  qc: QueryClient,
  wsId: string,
): [QueryKey, TaskFlatCache][] {
  return qc
    .getQueriesData<TaskFlatCache>({ queryKey: taskKeys.flatAll(wsId) })
    .filter(
      (entry): entry is [QueryKey, TaskFlatCache] =>
        !!entry[1] && Array.isArray(entry[1].pages),
    );
}

function tableRowEntries(
  qc: QueryClient,
  wsId: string,
): [QueryKey, TaskTableRowCache][] {
  return qc
    .getQueriesData<unknown>({ queryKey: taskKeys.tableAll(wsId) })
    .filter(
      (entry): entry is [QueryKey, TaskTableRowCache] =>
        !!entry[1] &&
        typeof entry[1] === "object" &&
        Array.isArray((entry[1] as TaskTableRowCache).rows),
    );
}

function flatContractFromKey(key: QueryKey): {
  scope: string | undefined;
  filter: TaskFlatFilter;
  sort: TaskSortParam;
} {
  return {
    scope: typeof key[3] === "string" ? key[3] : undefined,
    filter: (key[4] ?? {}) as TaskFlatFilter,
    sort: (key[5] ?? {}) as TaskSortParam,
  };
}

function patchFieldChanged<K extends keyof Task>(
  patch: Partial<Task>,
  base: Task | undefined,
  field: K,
) {
  if (!Object.prototype.hasOwnProperty.call(patch, field)) return false;
  return !base || !Object.is(patch[field], base[field]);
}

/** Whether the patch changes at least one task field relative to `base`.
 *  Every persisted edit also advances `updated_at` server-side even though the
 *  optimistic request payload does not carry that timestamp, so this doubles
 *  as "did updated_at advance" for `updated_at`-sorted surfaces. */
function patchChangesAnyTaskField(
  patch: Partial<Task>,
  base: Task | undefined,
): boolean {
  return Object.keys(patch).some((field) =>
    patchFieldChanged(patch, base, field as keyof Task),
  );
}

/** Whether a patch can change a flat window's membership or ordering. A
 * loaded row is always patched optimistically; only windows whose server
 * contract depends on the changed field need the follow-up refetch. */
function flatWindowNeedsReconcile(
  key: QueryKey,
  patch: Partial<Task>,
  base: Task | undefined,
  changed: TaskChangedDims,
) {
  const { scope, filter, sort } = flatContractFromKey(key);
  const anyTaskFieldChanged = patchChangesAnyTaskField(patch, base);

  if (listFilterDependsOn(scope, filter, changed)) return true;
  if (filter.q && patchFieldChanged(patch, base, "title")) return true;
  if (changed.status && (filter.statuses?.length ?? 0) > 0) return true;
  if (
    patchFieldChanged(patch, base, "priority") &&
    (filter.priorities?.length ?? 0) > 0
  ) {
    return true;
  }
  if (
    changed.assignee &&
    ((filter.assignee_filters?.length ?? 0) > 0 || filter.include_no_assignee)
  ) {
    return true;
  }
  if (changed.project && ((filter.project_ids?.length ?? 0) > 0 || filter.include_no_project)) {
    return true;
  }
  if (patchFieldChanged(patch, base, "parent_task_id") && filter.top_level_only) {
    return true;
  }
  if (sort.date_field === "updated_at" && anyTaskFieldChanged) return true;
  if (
    sort.date_field === "created_at" &&
    patchFieldChanged(patch, base, "created_at")
  ) {
    return true;
  }

  switch (sort.sort_by ?? "position") {
    case "title":
      return patchFieldChanged(patch, base, "title");
    case "status":
      return changed.status;
    case "priority":
      return patchFieldChanged(patch, base, "priority");
    case "created_at":
      return patchFieldChanged(patch, base, "created_at");
    case "updated_at":
      // Every persisted task edit advances updated_at even though the
      // optimistic request payload does not carry the server timestamp.
      return anyTaskFieldChanged;
    case "start_date":
      return patchFieldChanged(patch, base, "start_date");
    case "due_date":
      return patchFieldChanged(patch, base, "due_date");
    case "position":
      return patchFieldChanged(patch, base, "position");
    default:
      // Custom-property ordering is reconciled by the dedicated property
      // mutation/WS pipeline, which has the full property-bag snapshot.
      return false;
  }
}

export function applyTaskChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  patch: Partial<Task>,
  opts: {
    /** Which membership dimensions this change actually moved — compute via
     *  `taskChangedDims` (mutations) or the server's WS flags. */
    changed: TaskChangedDims;
    /** Freshest full pre-change entity, used to judge membership for lists
     *  where the card is not loaded. Omitting it degrades those judgments to
     *  "unknown" → a deferred refetch, never a wrong patch. */
    baseTask?: Task;
  },
): TaskCacheChangeResult {
  const { changed, baseTask } = opts;
  const prevLists: [QueryKey, ListTasksCache][] = [];
  const prevFlatLists: [QueryKey, TaskFlatCache][] = [];
  const prevTableRows: [QueryKey, TaskTableRowCache][] = [];
  const staleKeys: QueryKey[] = [];
  let prevTask: Task | undefined = baseTask;

  for (const [key, data] of bucketedListEntries(qc, wsId)) {
    const { scope, filter, sort } = listContractFromKey(key);
    const loc = findTaskLocation(data, id);
    const filterTouched = listFilterDependsOn(scope, filter, changed);

    // "Updated date" sort: every persisted edit advances updated_at, but the
    // optimistic patch carries no server timestamp and a loaded card keeps its
    // slot, so the board has drifted out of order. The right slot is server
    // knowledge — mark the key stale so the refetch re-sorts it. This mirrors
    // the updated_at case in flatWindowNeedsReconcile and, like it, does not
    // gate on this list's membership (an off-window member should surface too).
    if (
      sort.sort_by === "updated_at" &&
      patchChangesAnyTaskField(patch, loc?.task ?? baseTask)
    ) {
      staleKeys.push(key);
    }

    if (loc) {
      if (!prevTask) prevTask = loc.task;
      let next: ListTasksCache;
      if (filterTouched) {
        const membership = taskMatchesListFilter(
          { ...loc.task, ...patch },
          scope,
          filter,
        );
        if (membership === false) {
          next = removeTaskFromBuckets(data, id);
        } else {
          next = patchTaskInBuckets(data, id, patch);
          if (membership === "unknown") staleKeys.push(key);
        }
      } else {
        next = patchTaskInBuckets(data, id, patch);
      }
      if (next !== data) {
        prevLists.push([key, data]);
        qc.setQueryData<ListTasksCache>(key, next);
      }
      continue;
    }

    // Card not loaded here. Only a change that can move the task across
    // this list's filter — or shift a per-status count for an task beyond
    // the loaded window — needs a reconcile; anything else is a no-op.
    if (!filterTouched && !changed.status) continue;
    const wasMember = baseTask
      ? taskMatchesListFilter(baseTask, scope, filter)
      : "unknown";
    const isMember = taskMatchesListFilter(
      { ...baseTask, ...patch },
      scope,
      filter,
    );
    // Neither before nor after the change does this task belong to the
    // list — its pages and counts are untouched.
    if (wasMember === false && isMember === false) continue;

    // Certain count arithmetic — branch on the membership OUTCOME, never on
    // which field changed, so status / assignee / project (and future team)
    // all flow through the same two cases. wasMember === true implies a
    // baseTask exists, so the old status is known.
    if (wasMember === true && baseTask) {
      if (isMember === true) {
        // Still a member. Only a status change moves a count between
        // buckets; anything else (e.g. member→member reassignment) leaves
        // this list's pages and counts untouched.
        if (!changed.status || patch.status === undefined) continue;
        const next = moveBucketTotal(data, baseTask.status, patch.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListTasksCache>(key, next);
          // The count moved but the row can't be inserted client-side (its
          // page/slot under the list's sort is server knowledge). Leave the
          // reconcile trigger: with staleTime: Infinity nothing else ever
          // brings the destination bucket's window in line with its total.
          staleKeys.push(key);
        }
        continue;
      }
      if (isMember === false) {
        // Left the list entirely — the bucket it was counted in loses one.
        const next = decrementBucketTotal(data, baseTask.status);
        if (next !== data) {
          prevLists.push([key, data]);
          qc.setQueryData<ListTasksCache>(key, next);
        }
        continue;
      }
    }
    // Entering (its page/slot under the list's sort is server knowledge) or
    // any uncertainty (no base, unknown membership) → refetch instead of
    // guessing.
    staleKeys.push(key);
  }

  // Patch loaded flat rows immediately. Only refetch windows whose encoded
  // server filter/sort actually depends on this change; e.g. a title edit in
  // a position-sorted unfiltered table is fully reconciled by the patch.
  for (const [key, data] of flatListEntries(qc, wsId)) {
    let found: Task | undefined;
    const pages = data.pages.map((page) => ({
      ...page,
      tasks: page.tasks.map((task) => {
        if (task.id !== id) return task;
        found = task;
        return { ...task, ...patch };
      }),
    }));
    if (found) {
      if (!prevTask) prevTask = found;
      prevFlatLists.push([key, data]);
      qc.setQueryData<TaskFlatCache>(key, { ...data, pages });
    }
    if (flatWindowNeedsReconcile(key, patch, found ?? baseTask, changed)) {
      staleKeys.push(key);
    }
  }

  // Table branch pages are partial server-owned projections, so membership,
  // counts and ordering still reconcile through the existing tableAll
  // invalidation on settle. The entity snapshot inside every loaded row is
  // determinate, though: patch it immediately so inline edits never flash the
  // old title/status/assignee while the authoritative branch refetch runs.
  for (const [key, data] of tableRowEntries(qc, wsId)) {
    let found: Task | undefined;
    const rows = data.rows.map((row) => {
      if (row.task.id !== id) return row;
      found = row.task;
      return { ...row, task: { ...row.task, ...patch } };
    });
    if (!found) continue;
    if (!prevTask) prevTask = found;
    prevTableRows.push([key, data]);
    qc.setQueryData<TaskTableRowCache>(key, { ...data, rows });
  }

  const prevDetail = qc.getQueryData<Task>(taskKeys.detail(wsId, id));
  if (prevDetail) {
    qc.setQueryData<Task>(taskKeys.detail(wsId, id), {
      ...prevDetail,
      ...patch,
    });
    if (!prevTask) prevTask = prevDetail;
  }

  // Inbox rows carry an `task_status` display snapshot; the task's status
  // is the real state, so the projection follows every status write.
  let prevInboxList: InboxItem[] | undefined;
  if (patch.status !== undefined) {
    prevInboxList = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    if (prevInboxList) patchInboxTaskStatus(qc, wsId, id, patch.status);
  }

  return {
    prevLists,
    prevFlatLists,
    prevTableRows,
    prevDetail,
    prevInboxList,
    staleKeys,
    prevTask,
  };
}

/** Restore every snapshot captured by {@link applyTaskChange} — the onError
 *  leg of the optimistic lifecycle. */
export function rollbackTaskChange(
  qc: QueryClient,
  wsId: string,
  id: string,
  result: Pick<
    TaskCacheChangeResult,
    | "prevLists"
    | "prevFlatLists"
    | "prevTableRows"
    | "prevDetail"
    | "prevInboxList"
  >,
) {
  for (const [key, snapshot] of result.prevLists) {
    qc.setQueryData(key, snapshot);
  }
  for (const [key, snapshot] of result.prevFlatLists) {
    qc.setQueryData(key, snapshot);
  }
  for (const [key, snapshot] of result.prevTableRows) {
    qc.setQueryData(key, snapshot);
  }
  if (result.prevDetail !== undefined) {
    qc.setQueryData(taskKeys.detail(wsId, id), result.prevDetail);
  }
  if (result.prevInboxList !== undefined) {
    qc.setQueryData(inboxKeys.list(wsId), result.prevInboxList);
  }
}

/**
 * Refresh the aggregate projections a single-entity patch cannot recompute:
 * assignee-grouped boards (regrouping is server logic), every Project Gantt
 * (schedule membership + row mirrors), and project metrics when the change
 * could shift per-project counts.
 */
export function invalidateTaskDerivatives(
  qc: QueryClient,
  wsId: string,
  opts: { statusOrProjectChanged: boolean },
) {
  qc.invalidateQueries({ queryKey: taskKeys.assigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.myAssigneeGroupsAll(wsId) });
  qc.invalidateQueries({ queryKey: taskKeys.projectGanttAll(wsId) });
  if (opts.statusOrProjectChanged) {
    qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
  }
}

/** True when any object part of a query key encodes an "Updated date" ordering
 *  (`sort_by: "updated_at"`). Bucketed status boards and flat tables keep the
 *  sort in a standalone bag; assignee-grouped boards merge it into their filter
 *  bag (see `taskAssigneeGroupsOptions`). Scanning every object part matches
 *  all of those surfaces — workspace and My Tasks — with one rule. */
function queryKeyHasUpdatedAtSort(key: QueryKey): boolean {
  return key.some(
    (part) =>
      !!part &&
      typeof part === "object" &&
      !Array.isArray(part) &&
      (part as Record<string, unknown>).sort_by === "updated_at",
  );
}

/**
 * Refetch every loaded task list/board ordered by "Updated date" so a card
 * whose `updated_at` just advanced re-sorts to its true slot. Used by events
 * that bump `updated_at` without carrying the new timestamp or a field patch:
 * `comment:created` (SILI-5009) and the property/metadata WS events, all of
 * which advance the task's `updated_at` server-side but bypass the
 * coordinator's field-diff path. Covers status boards, flat tables, AND
 * assignee-grouped boards (workspace + My Tasks); only `updated_at`-sorted
 * keys are touched. The refetch is authoritative (server order + tie-breaks),
 * which also surfaces a touched card sitting beyond the loaded window.
 */
export function invalidateUpdatedAtSortedTaskLists(
  qc: QueryClient,
  wsId: string,
): void {
  qc.invalidateQueries({
    queryKey: taskKeys.all(wsId),
    predicate: (query) => queryKeyHasUpdatedAtSort(query.queryKey),
  });
}

/** Invalidate the stale keys reported by {@link applyTaskChange}, deduped —
 *  a batch over N tasks can report the same key N times. */
export function invalidateStaleListKeys(qc: QueryClient, staleKeys: QueryKey[]) {
  const seen = new Set<string>();
  for (const key of staleKeys) {
    const hash = hashKey(key);
    if (seen.has(hash)) continue;
    seen.add(hash);
    qc.invalidateQueries({ queryKey: key, exact: true });
  }
}
