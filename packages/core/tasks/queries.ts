import {
  infiniteQueryOptions,
  keepPreviousData,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import type {
  GroupedTasksResponse,
  Task,
  TaskStatus,
  TaskTableFacetsRequest,
  TaskTableGroupSpec,
  TaskTableGroupsRequest,
  TaskTableQuerySpec,
  TaskTableRowsRequest,
  ListGroupedTasksParams,
  ListTasksParams,
  ListTasksCache,
} from "../types";
import { ALL_STATUSES } from "./config";

export interface TaskSortParam {
  sort_by?: ListTasksParams["sort_by"];
  sort_direction?: ListTasksParams["sort_direction"];
  date_field?: ListTasksParams["date_field"];
  date_start?: ListTasksParams["date_start"];
  date_end?: ListTasksParams["date_end"];
  /** Server-side custom-property filter (definition id → accepted values).
   *  Lives in the sort/window bag so every list surface, query key, and
   *  load-more page carries it automatically. */
  properties?: ListTasksParams["properties"];
}

export const taskKeys = {
  all: (wsId: string) => ["tasks", wsId] as const,
  /** PREFIX for invalidation — no sort. */
  list: (wsId: string) => [...taskKeys.all(wsId), "list"] as const,
  /** FULL KEY for queryOptions — includes sort. */
  listSorted: (wsId: string, sort?: TaskSortParam) =>
    [...taskKeys.list(wsId), sort ?? {}] as const,
  flatAll: (wsId: string) => [...taskKeys.all(wsId), "flat"] as const,
  flat: (
    wsId: string,
    scope: string,
    filter: TaskFlatFilter,
    sort?: TaskSortParam,
  ) => [...taskKeys.flatAll(wsId), scope, filter, sort ?? {}] as const,
  flatExport: (
    wsId: string,
    scope: string,
    filter: TaskFlatFilter,
    sort?: TaskSortParam,
  ) => [...taskKeys.flatAll(wsId), "export", scope, filter, sort ?? {}] as const,
  tableAll: (wsId: string) => [...taskKeys.all(wsId), "table-query"] as const,
  tableGroups: (
    wsId: string,
    query: TaskTableQuerySpec,
    group: TaskTableGroupsRequest["group"],
  ) => [...taskKeys.tableAll(wsId), "groups", query, group] as const,
  tableFacets: (
    wsId: string,
    request: TaskTableFacetsRequest,
  ) => [...taskKeys.tableAll(wsId), "facets", request] as const,
  tableRows: (
    wsId: string,
    query: TaskTableQuerySpec,
    group: TaskTableGroupSpec,
    groupKey: string | null,
    hierarchy: boolean,
    parentId: string | null,
  ) => [
    ...taskKeys.tableAll(wsId),
    "rows",
    query,
    group,
    groupKey,
    { hierarchy, parentId },
  ] as const,
  assigneeGroupsAll: (wsId: string) =>
    [...taskKeys.all(wsId), "assignee-groups"] as const,
  assigneeGroups: (wsId: string, filter: AssigneeGroupedTasksFilter) =>
    [...taskKeys.assigneeGroupsAll(wsId), filter] as const,
  /** All "my tasks" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...taskKeys.all(wsId), "my"] as const,
  /** PREFIX for per-scope invalidation — no sort. */
  myList: (wsId: string, scope: string, filter: MyTasksFilter) =>
    [...taskKeys.myAll(wsId), scope, filter] as const,
  /** FULL KEY for queryOptions — includes sort. */
  myListSorted: (wsId: string, scope: string, filter: MyTasksFilter, sort?: TaskSortParam) =>
    [...taskKeys.myList(wsId, scope, filter), sort ?? {}] as const,
  myAssigneeGroupsAll: (wsId: string) =>
    [...taskKeys.myAll(wsId), "assignee-groups"] as const,
  myAssigneeGroups: (
    wsId: string,
    scope: string,
    filter: AssigneeGroupedTasksFilter,
  ) => [...taskKeys.myAssigneeGroupsAll(wsId), scope, filter] as const,
  /** All Project Gantt queries — prefix-match key for cross-project invalidation. */
  projectGanttAll: (wsId: string) =>
    [...taskKeys.all(wsId), "project-gantt"] as const,
  /**
   * Per-project Gantt task list (scheduled-only). Uses its own cache key
   * rather than reusing the bucketed `myList` cache so WS handlers and
   * cache helpers don't have to special-case a non-bucketed shape under
   * the `my` prefix.
   */
  projectGantt: (wsId: string, projectId: string) =>
    [...taskKeys.projectGanttAll(wsId), projectId] as const,
  detail: (wsId: string, id: string) =>
    [...taskKeys.all(wsId), "detail", id] as const,
  /** Resolve a bare task identifier (e.g. "SILI-123") to an task. */
  identifier: (wsId: string, identifier: string) =>
    [...taskKeys.all(wsId), "identifier", identifier] as const,
  /** Prefix for every per-parent children query in a workspace. */
  childrenAll: (wsId: string) =>
    [...taskKeys.all(wsId), "children"] as const,
  children: (wsId: string, id: string) =>
    [...taskKeys.childrenAll(wsId), id] as const,
  /** Prefix for invalidating all batched-children queries in a workspace. */
  childrenByParentsAll: (wsId: string) =>
    [...taskKeys.all(wsId), "children-by-parents"] as const,
  /** Full key — includes sorted parent ids for cache stability. */
  childrenByParents: (wsId: string, parentIds: readonly string[]) =>
    [...taskKeys.childrenByParentsAll(wsId), parentIds] as const,
  childProgress: (wsId: string) =>
    [...taskKeys.all(wsId), "child-progress"] as const,
  /** Prefix-match keys for invalidating the per-task caches below across
   *  all tasks. These keys carry no wsId, so `taskKeys.all(wsId)` does NOT
   *  cover them — WS reconnect recovery must invalidate these `*All`
   *  prefixes explicitly, or missed events leave them stale forever under
   *  the staleTime: Infinity default (#3953). */
  timelineAll: () => ["tasks", "timeline"] as const,
  /** Full-task timeline (single TanStack Query, no cursor). */
  timeline: (taskId: string) =>
    [...taskKeys.timelineAll(), taskId] as const,
  /** Prefix across all tasks — WS task lifecycle events invalidate here so
   *  an open composer's trigger preview refreshes when an agent's queue
   *  state changes (the dedup guard makes the answer queue-dependent). */
  commentTriggerPreviewAll: () => ["tasks", "comment-trigger-preview"] as const,
  /** PREFIX for invalidation — the composer hook appends parent + content signature. */
  commentTriggerPreview: (taskId: string) =>
    [...taskKeys.commentTriggerPreviewAll(), taskId] as const,
  /** Prefix across all task-trigger previews (assign/status/create/batch).
   *  WS task lifecycle events invalidate here so the answer revalidates when an
   *  agent's queue state changes (the status source's pending dedup makes it
   *  queue-dependent, mirroring commentTriggerPreviewAll). */
  taskTriggerPreviewAll: () => ["tasks", "task-trigger-preview"] as const,
  /** PREFIX — the picker hook appends a signature of the prospective write. */
  taskTriggerPreview: (signature: string) =>
    [...taskKeys.taskTriggerPreviewAll(), signature] as const,
  reactionsAll: () => ["tasks", "reactions"] as const,
  reactions: (taskId: string) =>
    [...taskKeys.reactionsAll(), taskId] as const,
  subscribersAll: () => ["tasks", "subscribers"] as const,
  subscribers: (taskId: string) =>
    [...taskKeys.subscribersAll(), taskId] as const,
  usageAll: () => ["tasks", "usage"] as const,
  usage: (taskId: string) => [...taskKeys.usageAll(), taskId] as const,
  attachmentsAll: () => ["tasks", "attachments"] as const,
  /** Task-level attachments — used by the description editor so its
   *  inline file-card / image NodeViews can re-sign download URLs at
   *  click time. */
  attachments: (taskId: string) =>
    [...taskKeys.attachmentsAll(), taskId] as const,

};

export type MyTasksFilter = Pick<
  ListTasksParams,
  | "assignee_id"
  | "assignee_ids"
  | "assignee_types"
  | "creator_id"
  | "project_id"
  | "involves_user_id"
>;

/** Server-side contract for the flat table window. These facets must travel
 * with every offset page (and live in the query key); post-filtering a loaded
 * page can silently omit matching tasks after the current offset. */
export type TaskFlatFilter = MyTasksFilter &
  Pick<
    ListTasksParams,
    | "q"
    | "statuses"
    | "priorities"
    | "assignee_filters"
    | "include_no_assignee"
    | "creator_filters"
    | "project_ids"
    | "include_no_project"
    | "label_ids"
    | "top_level_only"
    | "ids"
  >;

export type AssigneeGroupedTasksFilter = Omit<
  ListGroupedTasksParams,
  "group_by" | "limit" | "offset" | "group_assignee_type" | "group_assignee_id"
>;

/** Page size per status column. */
export const TASK_PAGE_SIZE = 50;
export const TASK_FLAT_PAGE_SIZE = 100;

/**
 * Statuses fetched and paginated into the list/board cache — every lifecycle
 * status, `cancelled` included. `cancelled` is a first-class default status
 * (SILI-4290), so it lives in the cache and renders like any other column;
 * there is no separate "visible board" subset. This constant governs
 * fetch/cache membership.
 */
export const PAGINATED_STATUSES: readonly TaskStatus[] = ALL_STATUSES;

/** Flatten a bucketed response to a single Task[] for consumers that want the whole list. */
export function flattenTaskBuckets(data: ListTasksCache) {
  const out = [];
  for (const status of PAGINATED_STATUSES) {
    const bucket = data.byStatus[status];
    if (bucket) out.push(...bucket.tasks);
  }
  return out;
}

async function fetchFirstPages(filter: MyTasksFilter = {}, sort?: TaskSortParam): Promise<ListTasksCache> {
  const responses = await Promise.all(
    PAGINATED_STATUSES.map((status) =>
      api.listTasks({ status, limit: TASK_PAGE_SIZE, offset: 0, ...sort, ...filter }),
    ),
  );
  const byStatus: ListTasksCache["byStatus"] = {};
  PAGINATED_STATUSES.forEach((status, i) => {
    const res = responses[i]!;
    byStatus[status] = { tasks: res.tasks, total: res.total };
  });
  return { byStatus };
}

/**
 * "All my tasks" — union of three server filters:
 *   assignee_id=me OR creator_id=me OR involves_user_id=me
 *
 * The backend has no OR-across-user-filters today, so we run the three
 * existing single-filter fetches in parallel and dedupe on the client by
 * task id within each status bucket. Order within each bucket preserves
 * the first-seen position (each sub-fetch is already server-sorted).
 *
 * Personal lists are bounded (tens to a few hundred tasks across all
 * three relations), so 3× the request count is acceptable — a single
 * fetchFirstPages already runs 7 status fetches in parallel, so the total
 * here is 21 small parallel requests. Easy enough; no need to add a new
 * backend query just for this scope.
 *
 * `total` per bucket is set to the merged length, not the true server
 * total — pagination on the "All" scope is out of scope; the first
 * 50-per-status × 3 widening (deduped) is what the page renders.
 */
const MERGE_PRIORITY_RANK: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3, none: 4 };
const MERGE_STATUS_RANK: Record<string, number> = {
  backlog: 0,
  todo: 1,
  in_progress: 2,
  in_review: 3,
  done: 4,
  blocked: 5,
  cancelled: 6,
};

/**
 * Comparator mirroring the server's ORDER BY semantics (including
 * `property:<id>` sorts and missing-values-last). The merged "All" scope
 * concatenates three independently-ordered queries, so without a re-sort the
 * relation order (assigned → created → involves) would override the sort the
 * user picked — e.g. assigned=9 rendering before created=1 (review round 3).
 */
export function compareTasksForSort(a: Task, b: Task, sort?: TaskSortParam): number {
  const by = sort?.sort_by ?? "position";
  const dir = by !== "position" && sort?.sort_direction === "desc" ? -1 : 1;
  // created_at DESC then id DESC, mirroring the server's unique ORDER BY
  // suffix — ids disambiguate bulk-created tasks that share a timestamp.
  const tieBreak = () =>
    new Date(b.created_at).getTime() - new Date(a.created_at).getTime() ||
    (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

  const missingAware = (av: string | null, bv: string | null): number => {
    if (!av && !bv) return tieBreak();
    if (!av) return 1;
    if (!bv) return -1;
    return dir * av.localeCompare(bv) || tieBreak();
  };

  if (by.startsWith("property:")) {
    const propertyId = by.slice("property:".length);
    const av = a.properties?.[propertyId];
    const bv = b.properties?.[propertyId];
    const aMissing = av === undefined || Array.isArray(av) || typeof av === "boolean";
    const bMissing = bv === undefined || Array.isArray(bv) || typeof bv === "boolean";
    if (aMissing && bMissing) return tieBreak();
    if (aMissing) return 1;
    if (bMissing) return -1;
    if (typeof av === "number" && typeof bv === "number") return dir * (av - bv) || tieBreak();
    return dir * String(av).localeCompare(String(bv)) || tieBreak();
  }
  switch (by) {
    case "status":
      return dir * ((MERGE_STATUS_RANK[a.status] ?? 9) - (MERGE_STATUS_RANK[b.status] ?? 9)) || tieBreak();
    case "priority":
      return dir * ((MERGE_PRIORITY_RANK[a.priority] ?? 9) - (MERGE_PRIORITY_RANK[b.priority] ?? 9)) || tieBreak();
    case "title":
      return dir * a.title.localeCompare(b.title) || tieBreak();
    case "created_at":
      return dir * (new Date(a.created_at).getTime() - new Date(b.created_at).getTime()) || tieBreak();
    case "updated_at":
      return dir * (new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()) || tieBreak();
    case "start_date":
      return missingAware(a.start_date, b.start_date);
    case "due_date":
      return missingAware(a.due_date, b.due_date);
    case "position":
    default:
      return a.position - b.position || tieBreak();
  }
}

async function fetchAllFlatPages(
  filter: TaskFlatFilter,
  sort?: TaskSortParam,
): Promise<Task[]> {
  const tasks: Task[] = [];
  const seenIds = new Set<string>();
  let offset = 0;
  while (true) {
    const response = await api.listTasks({
      ...filter,
      ...sort,
      limit: TASK_FLAT_PAGE_SIZE,
      offset,
    });
    let added = 0;
    for (const task of response.tasks) {
      if (seenIds.has(task.id)) continue;
      seenIds.add(task.id);
      tasks.push(task);
      added += 1;
    }
    if (tasks.length >= response.total) break;
    if (response.tasks.length === 0 || added === 0) {
      throw new Error("Task export pagination did not advance");
    }
    // Advance by what the server actually returned. This guarantees progress
    // even if an older server clamps the requested page size differently.
    offset += response.tasks.length;
  }
  return tasks;
}

async function fetchAllMyFlatTasks(
  userId: string,
  filter: TaskFlatFilter,
  sort?: TaskSortParam,
): Promise<Task[]> {
  const relations = await Promise.all([
    fetchAllFlatPages({ ...filter, assignee_id: userId }, sort),
    fetchAllFlatPages({ ...filter, creator_id: userId }, sort),
    fetchAllFlatPages({ ...filter, involves_user_id: userId }, sort),
  ]);
  const byId = new Map<string, Task>();
  for (const tasks of relations) {
    for (const task of tasks) byId.set(task.id, task);
  }
  return [...byId.values()].sort((a, b) => compareTasksForSort(a, b, sort));
}

export function taskFlatListOptions(
  wsId: string,
  scope: string,
  filter: TaskFlatFilter,
  userId?: string,
  sort?: TaskSortParam,
) {
  const allMyTasks = scope === "all" && !!userId;
  return infiniteQueryOptions({
    queryKey: taskKeys.flat(wsId, scope, filter, sort),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      if (allMyTasks) {
        const tasks = await fetchAllMyFlatTasks(userId, filter, sort);
        return { tasks, total: tasks.length };
      }
      return api.listTasks({
        ...filter,
        ...sort,
        limit: TASK_FLAT_PAGE_SIZE,
        offset: pageParam,
      });
    },
    getNextPageParam: (lastPage, allPages) => {
      if (allMyTasks) return undefined;
      const loaded = allPages.reduce((count, page) => count + page.tasks.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    placeholderData: keepPreviousData,
  });
}

export function taskTableGroupsOptions(
  wsId: string,
  query: TaskTableQuerySpec,
  group: TaskTableGroupsRequest["group"],
) {
  return infiniteQueryOptions({
    queryKey: taskKeys.tableGroups(wsId, query, group),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) =>
      api.listTaskTableGroups({
        query,
        group,
        page: { limit: 100, cursor: pageParam },
    }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** One independently-addressable Table branch page.
 *
 * Table keeps every cursor page under its own query key so a refreshed head
 * can detach stale tail cursors before their responses land. Keep the request,
 * key shape, transition placeholder, and retry policy together here — the
 * dynamic branch graph in views should not duplicate this API contract. */
export function taskTableRowPageOptions(
  wsId: string,
  request: TaskTableRowsRequest,
) {
  const cursor = request.page?.cursor ?? null;
  return queryOptions({
    queryKey: [
      ...taskKeys.tableRows(
        wsId,
        request.query,
        request.group,
        request.group_key,
        request.hierarchy.enabled,
        request.parent_id,
      ),
      "page",
      cursor,
    ] as const,
    queryFn: () => api.listTaskTableRows(request),
    placeholderData: keepPreviousData,
    retry: false,
    // Dynamic useQueries observers detach/reinstall as sibling branches enter
    // and leave the viewport. Keep an errored page errored until the user hits
    // Retry — but still let a successful-but-invalidated page refetch on
    // reattach, so a row page that gets WS-invalidated while its observer is
    // detached doesn't stay stranded stale under the global `staleTime: Infinity`
    // default (correct count, missing task).
    //
    // Both guards are needed: `retryOnMount` covers a first-load error (no
    // data), while the `refetchOnMount` guard covers a background-refetch error
    // on a page that still holds data — TanStack flags such a page
    // `isInvalidated`, so a plain `refetchOnMount: true` would re-fire the
    // failing request on every reattach instead of waiting for explicit Retry.
    retryOnMount: false,
    refetchOnMount: (query) => query.state.status !== "error",
  });
}

export function taskTableFacetsOptions(
  wsId: string,
  request: TaskTableFacetsRequest,
) {
  return queryOptions({
    queryKey: taskKeys.tableFacets(wsId, request),
    queryFn: () => api.listTaskTableFacets(request),
  });
}

export function taskFlatExportOptions(
  wsId: string,
  scope: string,
  filter: TaskFlatFilter,
  userId?: string,
  sort?: TaskSortParam,
) {
  return queryOptions({
    queryKey: taskKeys.flatExport(wsId, scope, filter, sort),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyFlatTasks(userId, filter, sort)
        : fetchAllFlatPages(filter, sort),
    staleTime: 0,
  });
}

async function fetchAllMyFirstPages(userId: string, sort?: TaskSortParam): Promise<ListTasksCache> {
  const [byAssignee, byCreator, byInvolves] = await Promise.all([
    fetchFirstPages({ assignee_id: userId }, sort),
    fetchFirstPages({ creator_id: userId }, sort),
    fetchFirstPages({ involves_user_id: userId }, sort),
  ]);
  const byStatus: ListTasksCache["byStatus"] = {};
  for (const status of PAGINATED_STATUSES) {
    const seen = new Set<string>();
    const merged: Task[] = [];
    for (const cache of [byAssignee, byCreator, byInvolves]) {
      const bucket = cache.byStatus[status];
      if (!bucket) continue;
      for (const task of bucket.tasks) {
        if (seen.has(task.id)) continue;
        seen.add(task.id);
        merged.push(task);
      }
    }
    merged.sort((a, b) => compareTasksForSort(a, b, sort));
    byStatus[status] = { tasks: merged, total: merged.length };
  }
  return { byStatus };
}

/**
 * Sibling of {@link fetchAllMyFirstPages} for the assignee-grouped board
 * view. Runs the three single-filter grouped queries in parallel and
 * merges groups by (assignee_type, assignee_id), deduping tasks within
 * each group. Extra filters from the page (statuses, priorities, etc.)
 * pass through unchanged.
 */
async function fetchAllMyAssigneeGroups(
  userId: string,
  filter: AssigneeGroupedTasksFilter,
  sort?: TaskSortParam,
): Promise<GroupedTasksResponse> {
  const variants: AssigneeGroupedTasksFilter[] = [
    { ...filter, assignee_id: userId },
    { ...filter, creator_id: userId },
    { ...filter, involves_user_id: userId },
  ];
  const responses = await Promise.all(
    variants.map((f) =>
      api.listGroupedTasks({
        group_by: "assignee",
        limit: TASK_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...f,
      }),
    ),
  );
  const groupKey = (g: GroupedTasksResponse["groups"][number]) =>
    `${g.assignee_type ?? "_"}::${g.assignee_id ?? "_"}`;
  const merged = new Map<string, GroupedTasksResponse["groups"][number]>();
  for (const res of responses) {
    for (const group of res.groups) {
      const key = groupKey(group);
      const existing = merged.get(key);
      if (!existing) {
        merged.set(key, {
          ...group,
          tasks: [...group.tasks],
          total: group.tasks.length,
        });
        continue;
      }
      const seen = new Set(existing.tasks.map((i) => i.id));
      for (const task of group.tasks) {
        if (seen.has(task.id)) continue;
        seen.add(task.id);
        existing.tasks.push(task);
      }
      existing.total = existing.tasks.length;
    }
  }
  const groups = [...merged.values()];
  for (const group of groups) {
    group.tasks.sort((a, b) => compareTasksForSort(a, b, sort));
  }
  return { groups };
}

/**
 * CACHE SHAPE NOTE: The raw cache stores {@link ListTasksCache} (buckets keyed
 * by status, each with `{ tasks, total }`), and `select` flattens it to
 * `Task[]` for consumers. Mutations and ws-updaters must use
 * `setQueryData<ListTasksCache>(...)` and preserve the byStatus shape.
 *
 * Fetches the first page of each paginated status in parallel. Use
 * {@link useLoadMoreByStatus} to paginate a specific status into the cache.
 */
export function taskListOptions(wsId: string, sort?: TaskSortParam) {
  return queryOptions({
    queryKey: taskKeys.listSorted(wsId, sort),
    queryFn: () => fetchFirstPages({}, sort),
    select: flattenTaskBuckets,
    placeholderData: keepPreviousData,
  });
}

export function taskAssigneeGroupsOptions(
  wsId: string,
  filter: AssigneeGroupedTasksFilter,
  sort?: TaskSortParam,
) {
  return queryOptions<GroupedTasksResponse>({
    queryKey: taskKeys.assigneeGroups(wsId, { ...filter, ...sort }),
    queryFn: () =>
      api.listGroupedTasks({
        group_by: "assignee",
        limit: TASK_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...filter,
      }),
    placeholderData: keepPreviousData,
  });
}

/**
 * Server-filtered task list for the My Tasks page.
 * Each scope gets its own cache entry so switching tabs is instant after first load.
 */
export function myTaskListOptions(
  wsId: string,
  scope: string,
  filter: MyTasksFilter,
  // Required when scope === "all" — the user id whose three relations
  // (assignee, creator, agents+squads) we union over. For every other
  // scope the filter object already carries the relevant id and userId
  // is ignored.
  userId?: string,
  sort?: TaskSortParam,
) {
  return queryOptions({
    queryKey: taskKeys.myListSorted(wsId, scope, filter, sort),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyFirstPages(userId, sort)
        : fetchFirstPages(filter, sort),
    select: flattenTaskBuckets,
    placeholderData: keepPreviousData,
  });
}

/**
 * Page size for the scheduled-task fetch. The Gantt view always pulls every
 * scheduled task (no client pagination), so this is just the chunk size we
 * use to walk the server's `(limit, offset)` window until we hit `total`.
 */
export const PROJECT_GANTT_PAGE_LIMIT = 500;

/**
 * Paranoia cap on the loop in {@link fetchProjectGanttTasks}. Real projects
 * shouldn't come close to this — a single project carrying 50k scheduled
 * tasks is already a product problem, not a Gantt-rendering one — but the
 * guard prevents a buggy server `total` from spinning the loop forever.
 */
export const PROJECT_GANTT_MAX_TASKS = 10_000;

async function fetchProjectGanttTasks(projectId: string) {
  const tasks = [];
  let offset = 0;
  while (offset < PROJECT_GANTT_MAX_TASKS) {
    const res = await api.listTasks({
      project_id: projectId,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset,
    });
    tasks.push(...res.tasks);
    if (res.tasks.length < PROJECT_GANTT_PAGE_LIMIT) break;
    if (tasks.length >= res.total) break;
    offset += PROJECT_GANTT_PAGE_LIMIT;
  }
  return tasks;
}

/**
 * One-shot fetch of every scheduled task (`start_date` or `due_date` set)
 * for a project. The Project Gantt view consumes this directly — no status
 * bucketing, no client-side pagination, no Load-all affordance — because
 * the scheduled subset is bounded enough to come back in a small handful of
 * requests.
 *
 * Backed by `GET /api/tasks?scheduled=true&project_id=…`; the SQL filter
 * mirrors the same `(start_date IS NOT NULL OR due_date IS NOT NULL)`
 * predicate the Gantt view applies on the client. Pages are walked until
 * `total` is reached so an oversized project can't silently lose bars past
 * the first page.
 */
export function projectGanttTasksOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: taskKeys.projectGantt(wsId, projectId),
    queryFn: () => fetchProjectGanttTasks(projectId),
  });
}

export function myTaskAssigneeGroupsOptions(
  wsId: string,
  scope: string,
  filter: AssigneeGroupedTasksFilter,
  // See myTaskListOptions for the userId contract — only consulted when
  // scope === "all", and powers the 3-fetch grouped union.
  userId?: string,
  sort?: TaskSortParam,
) {
  return queryOptions<GroupedTasksResponse>({
    queryKey: taskKeys.myAssigneeGroups(wsId, scope, { ...filter, ...sort }),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyAssigneeGroups(userId, filter, sort)
        : api.listGroupedTasks({
            group_by: "assignee",
            limit: TASK_PAGE_SIZE,
            offset: 0,
            ...sort,
            ...filter,
          }),
    placeholderData: keepPreviousData,
  });
}

export function taskDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: taskKeys.detail(wsId, id),
    queryFn: () => api.getTask(id),
  });
}

/**
 * Resolve a bare task identifier ("SILI-123") to its task, or `null`.
 *
 * Backs the Linear-style autolink: the backend `q` search matches an
 * identifier on task NUMBER only (prefix-agnostic — `SILI-123` and `TES-123`
 * both hit number 123), so the exact `identifier === value` filter here is
 * what enforces the workspace prefix. A non-existent or wrong-prefix
 * identifier resolves to `null` and renders as plain text.
 *
 * Server state → TanStack Query; the key includes `wsId` and the identifier,
 * so identical identifiers across the app share one request. Caller gates
 * `enabled` (identifier shape + workspace prefix).
 */
export function taskIdentifierOptions(wsId: string, identifier: string) {
  return queryOptions({
    queryKey: taskKeys.identifier(wsId, identifier),
    queryFn: async ({ signal }) => {
      const res = await api.searchTasks({
        q: identifier,
        limit: 10,
        include_closed: true,
        signal,
      });
      return res.tasks.find((i) => i.identifier === identifier) ?? null;
    },
    // Identifier→task mapping is effectively immutable; avoid refetch churn
    // when the same key renders across many comments/messages.
    staleTime: 5 * 60_000,
  });
}

export function childTaskProgressOptions(wsId: string) {
  return queryOptions({
    queryKey: taskKeys.childProgress(wsId),
    queryFn: () => api.getChildTaskProgress(),
    select: (data) => {
      const map = new Map<string, { done: number; total: number }>();
      for (const entry of data.progress) {
        map.set(entry.parent_task_id, { done: entry.done, total: entry.total });
      }
      return map;
    },
  });
}

export function childTasksOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: taskKeys.children(wsId, id),
    queryFn: () => api.listChildTasks(id).then((r) => r.tasks),
    // Child creation can happen while this workspace is not the active
    // realtime subscription (for example, an agent creates it while a
    // desktop tab is showing another workspace). The global Infinity
    // staleTime would otherwise reuse an incomplete children snapshot when
    // the parent is opened again, with no later event guaranteed to heal it.
    refetchOnMount: "always",
  });
}

/**
 * Server cap on parent_ids per `GET /api/tasks/children` request — must
 * match `listChildrenByParentsLimit` in core/internal/handler/task.go.
 * Exceeding it returns 400, so the client chunks larger requests.
 */
export const CHILDREN_BY_PARENTS_CHUNK_SIZE = 200;

/**
 * Batched variant of {@link childTasksOptions}: fetches children for all
 * given parents in `GET /api/tasks/children?parent_ids=…` requests, chunked
 * to {@link CHILDREN_BY_PARENTS_CHUNK_SIZE} parents each. The queryFn also
 * hydrates each parent's per-parent taskKeys.children cache so other
 * surfaces (task-detail sub-tasks panel, set-parent modal) hit the primed
 * cache instead of re-fetching. Hydration happens in queryFn (not a
 * useEffect) to avoid the setQueryData → re-render → effect loop.
 *
 * Used by SwimLaneView to resolve parent lanes without an N-request fan-out.
 * parentIds must be sorted + deduplicated by the caller for a stable cache key.
 */
async function fetchAndHydrateChildrenByParents(
  qc: QueryClient,
  wsId: string,
  parentIds: readonly string[],
) {
  // Chunk to respect the server cap (parallel, since chunks are independent).
  const chunks: string[][] = [];
  for (let i = 0; i < parentIds.length; i += CHILDREN_BY_PARENTS_CHUNK_SIZE) {
    chunks.push([...parentIds.slice(i, i + CHILDREN_BY_PARENTS_CHUNK_SIZE)]);
  }
  const responses = await Promise.all(chunks.map((c) => api.listChildrenByParents(c)));
  const grouped = new Map<string, Task[]>();
  for (const response of responses) {
    for (const task of response.tasks) {
      if (!task.parent_task_id) continue;
      const bucket = grouped.get(task.parent_task_id);
      if (bucket) {
        bucket.push(task);
      } else {
        grouped.set(task.parent_task_id, [task]);
      }
    }
  }
  for (const [parentId, children] of grouped) {
    // Only hydrate if the per-parent cache is empty — don't overwrite a
    // fresher result that another query (e.g. task-detail) may have written.
    // This relies on useUpdateTask.onMutate writing into the per-parent
    // cache (not creating an empty one) — if that contract changes, batch
    // hydration here would silently stop seeding new lanes.
    const existing = qc.getQueryData<Task[]>(taskKeys.children(wsId, parentId));
    if (!existing || existing.length === 0) {
      qc.setQueryData(taskKeys.children(wsId, parentId), children);
    }
  }
  return grouped;
}

export function childrenByParentsOptions(
  wsId: string,
  parentIds: readonly string[],
  qc: QueryClient,
) {
  return queryOptions({
    queryKey: taskKeys.childrenByParents(wsId, parentIds),
    queryFn: () => fetchAndHydrateChildrenByParents(qc, wsId, parentIds),
    enabled: parentIds.length > 0,
  });
}

/**
 * Single-fetch timeline options. The endpoint returns the full ordered set of
 * comments + activities for an task (server caps at 2000 as a safety net).
 * Cursor pagination was removed in #1929 — at observed data sizes (p99 ~30
 * entries per task) it added complexity without a UX win and broke reply
 * threads at page boundaries.
 */
export function taskTimelineOptions(taskId: string) {
  return queryOptions({
    queryKey: taskKeys.timeline(taskId),
    queryFn: () => api.listTimeline(taskId),
  });
}

export function taskReactionsOptions(taskId: string) {
  return queryOptions({
    queryKey: taskKeys.reactions(taskId),
    queryFn: async () => {
      const task = await api.getTask(taskId);
      return task.reactions ?? [];
    },
  });
}

export function taskSubscribersOptions(taskId: string) {
  return queryOptions({
    queryKey: taskKeys.subscribers(taskId),
    queryFn: () => api.listTaskSubscribers(taskId),
  });
}

export function taskUsageOptions(taskId: string) {
  return queryOptions({
    queryKey: taskKeys.usage(taskId),
    queryFn: () => api.getTaskUsage(taskId),
  });
}

// Backs the description editor's fresh-sign download flow: NodeViews resolve
// an attachment id by matching the markdown URL against this list. The list
// is workspace-private metadata and lives on the same cache lifetime as the
// rest of the task detail surface.
export function taskAttachmentsOptions(taskId: string) {
  return queryOptions({
    queryKey: taskKeys.attachments(taskId),
    queryFn: () => api.listAttachments(taskId),
  });
}
