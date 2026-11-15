import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type {
  Task,
  TaskTableRowsRequest,
  TaskTableRowsResponse,
  ListTasksParams,
  ListTasksResponse,
  SearchTasksResponse,
} from "../types";
import {
  CHILDREN_BY_PARENTS_CHUNK_SIZE,
  TASK_FLAT_PAGE_SIZE,
  PROJECT_GANTT_MAX_TASKS,
  PROJECT_GANTT_PAGE_LIMIT,
  childrenByParentsOptions,
  childTasksOptions,
  compareTasksForSort,
  taskFlatExportOptions,
  taskFlatListOptions,
  taskIdentifierOptions,
  taskKeys,
  taskTableRowPageOptions,
  projectGanttTasksOptions,
} from "./queries";

const WS_ID = "ws-1";
const PROJECT_ID = "project-1";

function makeTask(idx: number, overrides: Partial<Task> = {}): Task {
  return {
    id: `task-${idx}`,
    workspace_id: WS_ID,
    number: idx,
    identifier: `SILI-${idx}`,
    title: `Task ${idx}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: PROJECT_ID,
    position: idx,
    stage: null,
    start_date: "2026-05-01T00:00:00Z",
    due_date: null,
    labels: [],
    metadata: {},
  properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

// Type-only shim — only the methods the queries.ts code path under test calls.
function installFakeApi(listTasks: (params?: ListTasksParams) => Promise<ListTasksResponse>) {
  setApiInstance({ listTasks } as unknown as ApiClient);
}

function installFakeChildrenApi(
  listChildrenByParents: (parentIds: string[]) => Promise<{ tasks: Task[] }>,
) {
  setApiInstance({ listChildrenByParents } as unknown as ApiClient);
}

function installFakeChildApi(
  listChildTasks: (parentId: string) => Promise<{ tasks: Task[] }>,
) {
  setApiInstance({ listChildTasks } as unknown as ApiClient);
}

function installFakeSearchApi(
  searchTasks: (params: { q: string }) => Promise<SearchTasksResponse>,
) {
  setApiInstance({ searchTasks } as unknown as ApiClient);
}

function makeSearchResult(idx: number, identifier: string) {
  return { ...makeTask(idx), identifier, match_source: "title" as const };
}

describe("childTasksOptions", () => {
  it("refetches a cached snapshot when the parent task is opened again", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const parentId = "parent-1";
    const oldChild = makeTask(1, { parent_task_id: parentId });
    const newChild = makeTask(2, { parent_task_id: parentId });
    const listChildTasks = vi.fn().mockResolvedValue({
      tasks: [oldChild, newChild],
    });
    installFakeChildApi(listChildTasks);
    qc.setQueryData(taskKeys.children(WS_ID, parentId), [oldChild]);

    const observer = new QueryObserver(
      qc,
      childTasksOptions(WS_ID, parentId),
    );
    const unsubscribe = observer.subscribe(() => {});

    await vi.waitFor(() => {
      expect(listChildTasks).toHaveBeenCalledWith(parentId);
      expect(observer.getCurrentResult().data).toEqual([oldChild, newChild]);
    });

    unsubscribe();
    qc.clear();
  });
});

describe("taskTableRowPageOptions", () => {
  // Reproduces the "count correct, task missing until page refresh" bug: a row
  // page gets invalidated while its dynamic useQueries observer is detached, then
  // the observer reattaches. Under the global `staleTime: Infinity` default the
  // page is stale only because it is invalidated, so the reattaching observer
  // MUST refetch it. `refetchOnMount: false` used to suppress that refetch and
  // strand the row stale; `retryOnMount: false` does not.
  const request: TaskTableRowsRequest = {
    query: {
      scope: { kind: "workspace" },
      filters: {},
      sort: { field: "position", direction: "asc" },
    },
    group: { kind: "status" },
    group_key: "todo",
    hierarchy: { enabled: false },
    parent_id: null,
    page: { limit: 50, cursor: null },
  };

  function makeRowsResponse(tasks: Task[]): TaskTableRowsResponse {
    return {
      query_fingerprint: "fp",
      group_key: "todo",
      parent_id: null,
      total: tasks.length,
      rows: tasks.map((task) => ({ task, direct_child_count: 0 })),
      branch_total: tasks.length,
      next_cursor: null,
    };
  }

  function rowTaskIds(response: TaskTableRowsResponse | undefined): string[] {
    return response?.rows.map((row) => row.task.id) ?? [];
  }

  function installFakeTableRowsApi(
    listTaskTableRows: (
      params: TaskTableRowsRequest,
    ) => Promise<TaskTableRowsResponse>,
  ) {
    setApiInstance({ listTaskTableRows } as unknown as ApiClient);
  }

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("refetches an invalidated page when its observer reattaches", async () => {
    // Global default: server state stays fresh until explicitly invalidated.
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listTaskTableRows = vi
      .fn<
        (params: TaskTableRowsRequest) => Promise<TaskTableRowsResponse>
      >()
      // Head snapshot, then the post-move snapshot that includes the new task.
      .mockResolvedValueOnce(makeRowsResponse([makeTask(1)]))
      .mockResolvedValueOnce(makeRowsResponse([makeTask(1), makeTask(2)]));
    installFakeTableRowsApi(listTaskTableRows);

    const options = taskTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(listTaskTableRows).toHaveBeenCalledTimes(1);
      expect(rowTaskIds(observer1.getCurrentResult().data)).toEqual([
        "task-1",
      ]);
    });

    // Observer detaches (sibling branch left the viewport), then the row page is
    // invalidated while no observer is active — it only gets marked stale.
    unsubscribe1();
    await qc.invalidateQueries({ queryKey: options.queryKey });
    const cached = qc.getQueryState(options.queryKey);
    expect(cached?.isInvalidated).toBe(true);
    expect(cached?.fetchStatus).toBe("idle");

    // Observer reattaches: the invalidated page must refetch and pick up task-2.
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    await vi.waitFor(() => {
      expect(listTaskTableRows).toHaveBeenCalledTimes(2);
      expect(rowTaskIds(observer2.getCurrentResult().data)).toEqual([
        "task-1",
        "task-2",
      ]);
    });

    unsubscribe2();
    qc.clear();
  });

  it("keeps an errored page errored on reattach (no auto-retry)", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listTaskTableRows = vi
      .fn<
        (params: TaskTableRowsRequest) => Promise<TaskTableRowsResponse>
      >()
      .mockRejectedValue(new Error("boom"));
    installFakeTableRowsApi(listTaskTableRows);

    const options = taskTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("error");
    });
    expect(listTaskTableRows).toHaveBeenCalledTimes(1);
    unsubscribe1();

    // Reattaching an errored page stays idle — `retryOnMount: false` blocks the
    // automatic retry; only an explicit Retry re-runs it. The fetch decision is
    // synchronous, so the observer never enters `fetching`.
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    expect(observer2.getCurrentResult().status).toBe("error");
    expect(observer2.getCurrentResult().fetchStatus).toBe("idle");
    expect(listTaskTableRows).toHaveBeenCalledTimes(1);

    unsubscribe2();
    qc.clear();
  });

  it("does not auto-retry a background-refetch error on a page that still has data", async () => {
    // The tricky case: a page loads OK, then an invalidation-triggered background
    // refetch fails. TanStack flags such a page `isInvalidated: true` (see its
    // "error" reducer), so it is stale AND errored while keeping the old data. A
    // plain `refetchOnMount: true` would re-fire the failing request on every
    // observer reattach; `retryOnMount: false` alone does NOT cover this path
    // because it only guards no-data first-load errors. The `refetchOnMount`
    // status guard is what keeps the errored page stable until an explicit Retry.
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listTaskTableRows = vi
      .fn<
        (params: TaskTableRowsRequest) => Promise<TaskTableRowsResponse>
      >()
      .mockResolvedValueOnce(makeRowsResponse([makeTask(1)])) // initial load
      .mockRejectedValueOnce(new Error("refetch failed")) // background refetch
      .mockResolvedValueOnce(makeRowsResponse([makeTask(1), makeTask(2)])); // explicit Retry
    installFakeTableRowsApi(listTaskTableRows);

    const options = taskTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("success");
      expect(rowTaskIds(observer1.getCurrentResult().data)).toEqual([
        "task-1",
      ]);
    });

    // Invalidate while the observer is active: the background refetch fires and
    // fails, leaving the page errored-with-data and flagged invalidated.
    void qc.invalidateQueries({ queryKey: options.queryKey }).catch(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("error");
    });
    expect(listTaskTableRows).toHaveBeenCalledTimes(2);
    expect(rowTaskIds(observer1.getCurrentResult().data)).toEqual(["task-1"]);
    expect(qc.getQueryState(options.queryKey)?.isInvalidated).toBe(true);

    // Detach + reattach must NOT re-fire the failing request.
    unsubscribe1();
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    expect(observer2.getCurrentResult().status).toBe("error");
    expect(observer2.getCurrentResult().fetchStatus).toBe("idle");
    expect(listTaskTableRows).toHaveBeenCalledTimes(2);

    // Only an explicit Retry re-runs it — and then the fresh page renders.
    await observer2.refetch();
    expect(listTaskTableRows).toHaveBeenCalledTimes(3);
    expect(rowTaskIds(observer2.getCurrentResult().data)).toEqual([
      "task-1",
      "task-2",
    ]);

    unsubscribe2();
    qc.clear();
  });
});

describe("projectGanttTasksOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("returns the first page directly when it fits under PROJECT_GANTT_PAGE_LIMIT", async () => {
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockResolvedValue({
        tasks: [makeTask(1), makeTask(2)],
        total: 2,
      });
    installFakeApi(listTasks);

    const data = await qc.fetchQuery(projectGanttTasksOptions(WS_ID, PROJECT_ID));

    expect(listTasks).toHaveBeenCalledTimes(1);
    expect(listTasks).toHaveBeenCalledWith({
      project_id: PROJECT_ID,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset: 0,
    });
    expect(data).toHaveLength(2);
  });

  it("loops through pages until total is satisfied (no silent truncation)", async () => {
    const total = PROJECT_GANTT_PAGE_LIMIT + 7;
    const firstPage = Array.from({ length: PROJECT_GANTT_PAGE_LIMIT }, (_, i) =>
      makeTask(i),
    );
    const secondPage = Array.from({ length: 7 }, (_, i) =>
      makeTask(PROJECT_GANTT_PAGE_LIMIT + i),
    );

    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockImplementation(async (params) => {
        if (!params) throw new Error("expected params");
        const offset = params.offset ?? 0;
        if (offset === 0)
          return { tasks: firstPage, total };
        if (offset === PROJECT_GANTT_PAGE_LIMIT)
          return { tasks: secondPage, total };
        throw new Error(`unexpected offset ${offset}`);
      });
    installFakeApi(listTasks);

    const data = await qc.fetchQuery(projectGanttTasksOptions(WS_ID, PROJECT_ID));

    expect(listTasks).toHaveBeenCalledTimes(2);
    expect(data).toHaveLength(total);
  });

  it("stops looping when the server reports a smaller-than-limit page (safety net for total drift)", async () => {
    // Server says `total` is huge but only ever returns short pages — the
    // loop must terminate on the first short page to avoid an infinite fetch.
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockResolvedValue({
        tasks: [makeTask(1)],
        total: PROJECT_GANTT_MAX_TASKS,
      });
    installFakeApi(listTasks);

    const data = await qc.fetchQuery(projectGanttTasksOptions(WS_ID, PROJECT_ID));

    expect(listTasks).toHaveBeenCalledTimes(1);
    expect(data).toHaveLength(1);
  });

  it("uses the project-scoped Gantt cache key", () => {
    const options = projectGanttTasksOptions(WS_ID, PROJECT_ID);
    expect(options.queryKey).toEqual(taskKeys.projectGantt(WS_ID, PROJECT_ID));
  });
});

describe("flat task table queries", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("loads one offset page for the interactive table window", async () => {
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockResolvedValue({ tasks: [makeTask(1)], total: 1 });
    installFakeApi(listTasks);

    const data = await qc.fetchInfiniteQuery(
      taskFlatListOptions(
        WS_ID,
        "project:project-1",
        {
          project_id: PROJECT_ID,
          q: "release train",
          statuses: ["todo", "in_progress"],
          priorities: ["high"],
          assignee_filters: [{ type: "member", id: "member-1" }],
          include_no_assignee: true,
          creator_filters: [{ type: "agent", id: "agent-1" }],
          project_ids: ["project-2"],
          include_no_project: true,
          label_ids: ["label-1"],
          top_level_only: true,
        },
        undefined,
        { sort_by: "updated_at", sort_direction: "desc" },
      ),
    );

    expect(data.pages).toHaveLength(1);
    expect(data.pages[0]?.tasks.map((task) => task.id)).toEqual([
      "task-1",
    ]);
    expect(listTasks).toHaveBeenCalledWith({
      project_id: PROJECT_ID,
      q: "release train",
      statuses: ["todo", "in_progress"],
      priorities: ["high"],
      assignee_filters: [{ type: "member", id: "member-1" }],
      include_no_assignee: true,
      creator_filters: [{ type: "agent", id: "agent-1" }],
      project_ids: ["project-2"],
      include_no_project: true,
      label_ids: ["label-1"],
      top_level_only: true,
      sort_by: "updated_at",
      sort_direction: "desc",
      limit: TASK_FLAT_PAGE_SIZE,
      offset: 0,
    });
  });

  it("walks every page only for an explicit full CSV export", async () => {
    const first = Array.from({ length: TASK_FLAT_PAGE_SIZE }, (_, index) =>
      makeTask(index + 1),
    );
    const second = [makeTask(101), makeTask(102), makeTask(103)];
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockImplementation(async (params) => ({
        tasks: (params?.offset ?? 0) === 0 ? first : second,
        total: 103,
      }));
    installFakeApi(listTasks);

    const tasks = await qc.fetchQuery(
      taskFlatExportOptions(
        WS_ID,
        "project:project-1",
        { project_id: PROJECT_ID },
        undefined,
        { sort_by: "status", sort_direction: "asc" },
      ),
    );

    expect(tasks).toHaveLength(103);
    expect(listTasks).toHaveBeenCalledTimes(2);
    expect(listTasks.mock.calls.map(([params]) => params?.offset)).toEqual([
      0,
      TASK_FLAT_PAGE_SIZE,
    ]);
  });

  it("does not silently truncate exports above ten thousand tasks", async () => {
    const total = 10_001;
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockImplementation(async (params) => {
        const offset = params?.offset ?? 0;
        const count = Math.min(TASK_FLAT_PAGE_SIZE, total - offset);
        return {
          tasks: Array.from({ length: count }, (_, index) =>
            makeTask(offset + index + 1),
          ),
          total,
        };
      });
    installFakeApi(listTasks);

    const tasks = await qc.fetchQuery(
      taskFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
    );

    expect(tasks).toHaveLength(total);
    expect(listTasks).toHaveBeenCalledTimes(101);
    expect(listTasks.mock.calls.at(-1)?.[0]?.offset).toBe(10_000);
  });

  it("keeps paging when the server returns fewer rows than requested", async () => {
    const total = 101;
    const serverPageSize = 40;
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockImplementation(async (params) => {
        const offset = params?.offset ?? 0;
        const count = Math.min(serverPageSize, total - offset);
        return {
          tasks: Array.from({ length: count }, (_, index) =>
            makeTask(offset + index + 1),
          ),
          total,
        };
      });
    installFakeApi(listTasks);

    const tasks = await qc.fetchQuery(
      taskFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
    );

    expect(tasks).toHaveLength(total);
    expect(listTasks.mock.calls.map(([params]) => params?.offset)).toEqual([
      0, 40, 80,
    ]);
  });

  it("fails explicitly if the export endpoint stops advancing offsets", async () => {
    const page = Array.from({ length: TASK_FLAT_PAGE_SIZE }, (_, index) =>
      makeTask(index + 1),
    );
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockResolvedValue({ tasks: page, total: TASK_FLAT_PAGE_SIZE * 2 });
    installFakeApi(listTasks);

    await expect(
      qc.fetchQuery(
        taskFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
      ),
    ).rejects.toThrow("Task export pagination did not advance");
    expect(listTasks).toHaveBeenCalledTimes(2);
  });

  it("deduplicates the three My Tasks relations and restores global sort order", async () => {
    const shared = makeTask(1, { status: "done" });
    const backlog = makeTask(2, { status: "backlog" });
    const todo = makeTask(3, { status: "todo" });
    const listTasks = vi
      .fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>()
      .mockImplementation(async (params) => {
        if (params?.assignee_id) {
          return { tasks: [shared], total: 1 };
        }
        if (params?.creator_id) {
          return { tasks: [backlog, shared], total: 2 };
        }
        return { tasks: [todo], total: 1 };
      });
    installFakeApi(listTasks);

    const tasks = await qc.fetchQuery(
      taskFlatExportOptions(
        WS_ID,
        "all",
        {},
        "user-1",
        { sort_by: "status", sort_direction: "asc" },
      ),
    );

    expect(tasks.map((task) => task.id)).toEqual([
      backlog.id,
      todo.id,
      shared.id,
    ]);
    expect(listTasks).toHaveBeenCalledTimes(3);
  });
});

describe("compareTasksForSort tie-break", () => {
  it("orders equal sort values by created_at DESC then id DESC (server ORDER BY parity)", () => {
    // Same status AND same created_at — only the id disambiguates. Without a
    // unique final key the relative order would depend on input order, which
    // is exactly the instability that duplicates/drops rows at page
    // boundaries server-side.
    const first = makeTask(1, { created_at: "2025-01-01T00:00:00Z" });
    const second = makeTask(2, { created_at: "2025-01-01T00:00:00Z" });
    const sort = { sort_by: "status", sort_direction: "asc" } as const;

    expect(
      [first, second].sort((a, b) => compareTasksForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["task-2", "task-1"]);
    expect(
      [second, first].sort((a, b) => compareTasksForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["task-2", "task-1"]);
  });

  it("applies the id tie-break to created_at sorts as well", () => {
    const first = makeTask(1, { created_at: "2025-01-01T00:00:00Z" });
    const second = makeTask(2, { created_at: "2025-01-01T00:00:00Z" });
    const sort = { sort_by: "created_at", sort_direction: "desc" } as const;

    expect(
      [first, second].sort((a, b) => compareTasksForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["task-2", "task-1"]);
    expect(
      [second, first].sort((a, b) => compareTasksForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["task-2", "task-1"]);
  });
});

describe("childrenByParentsOptions chunking", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("tasks a single request when parentIds fit under the chunk size", async () => {
    const parentIds = Array.from({ length: 50 }, (_, i) => `p-${i}`);
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ tasks: Task[] }>>()
      .mockResolvedValue({ tasks: [] });
    installFakeChildrenApi(listChildrenByParents);

    await qc.fetchQuery(childrenByParentsOptions(WS_ID, parentIds, qc));

    expect(listChildrenByParents).toHaveBeenCalledTimes(1);
    expect(listChildrenByParents).toHaveBeenCalledWith(parentIds);
  });

  it("chunks parentIds into multiple requests when over the server cap", async () => {
    // 2.5 chunks worth of parents → 3 parallel requests.
    const count = CHILDREN_BY_PARENTS_CHUNK_SIZE * 2 + 17;
    const parentIds = Array.from({ length: count }, (_, i) => `p-${i}`);
    const calls: string[][] = [];
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ tasks: Task[] }>>()
      .mockImplementation(async (ids) => {
        calls.push(ids);
        return { tasks: [] };
      });
    installFakeChildrenApi(listChildrenByParents);

    await qc.fetchQuery(childrenByParentsOptions(WS_ID, parentIds, qc));

    expect(listChildrenByParents).toHaveBeenCalledTimes(3);
    expect(calls[0]).toHaveLength(CHILDREN_BY_PARENTS_CHUNK_SIZE);
    expect(calls[1]).toHaveLength(CHILDREN_BY_PARENTS_CHUNK_SIZE);
    expect(calls[2]).toHaveLength(17);
    // Together the chunks must cover every input parent id.
    expect(calls.flat().sort()).toEqual(parentIds.slice().sort());
  });

  it("merges children from all chunks into one grouped map", async () => {
    const parentIds = Array.from(
      { length: CHILDREN_BY_PARENTS_CHUNK_SIZE + 1 },
      (_, i) => `p-${i}`,
    );
    // First chunk returns a child of p-0, second chunk returns a child of
    // the last parent id (which lives alone in chunk 2).
    const lastId = parentIds[parentIds.length - 1]!;
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ tasks: Task[] }>>()
      .mockImplementation(async (ids) => {
        if (ids.includes(lastId)) {
          return { tasks: [{ ...makeTask(99), parent_task_id: lastId }] };
        }
        return { tasks: [{ ...makeTask(1), parent_task_id: "p-0" }] };
      });
    installFakeChildrenApi(listChildrenByParents);

    const grouped = await qc.fetchQuery(
      childrenByParentsOptions(WS_ID, parentIds, qc),
    );

    expect(grouped.get("p-0")).toHaveLength(1);
    expect(grouped.get(lastId)).toHaveLength(1);
  });
});

describe("taskIdentifierOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("returns the task whose identifier exactly matches the query", async () => {
    const searchTasks = vi
      .fn<(params: { q: string }) => Promise<SearchTasksResponse>>()
      .mockResolvedValue({
        tasks: [makeSearchResult(7, "SILI-7")],
        total: 1,
      });
    installFakeSearchApi(searchTasks);

    const data = await qc.fetchQuery(taskIdentifierOptions(WS_ID, "SILI-7"));

    expect(data?.id).toBe("task-7");
    expect(searchTasks).toHaveBeenCalledWith(
      expect.objectContaining({ q: "SILI-7" }),
    );
  });

  it("returns null when no result's identifier matches (wrong prefix / number-only hit)", async () => {
    // Backend number-match returns SILI-7 for a TES-7 query; exact filter rejects it.
    const searchTasks = vi
      .fn<(params: { q: string }) => Promise<SearchTasksResponse>>()
      .mockResolvedValue({
        tasks: [makeSearchResult(7, "SILI-7")],
        total: 1,
      });
    installFakeSearchApi(searchTasks);

    const data = await qc.fetchQuery(taskIdentifierOptions(WS_ID, "TES-7"));

    expect(data).toBeNull();
  });

  it("returns null on an empty (or malformed→empty) search response", async () => {
    const searchTasks = vi
      .fn<(params: { q: string }) => Promise<SearchTasksResponse>>()
      .mockResolvedValue({ tasks: [], total: 0 });
    installFakeSearchApi(searchTasks);

    const data = await qc.fetchQuery(taskIdentifierOptions(WS_ID, "SILI-999"));

    expect(data).toBeNull();
  });

  it("keys the query by workspace and identifier", () => {
    expect(taskKeys.identifier(WS_ID, "SILI-7")).toEqual([
      "tasks",
      WS_ID,
      "identifier",
      "SILI-7",
    ]);
  });
});
