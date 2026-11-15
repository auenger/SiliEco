/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { ApiError, setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import {
  useBatchUpdateTasks,
  useCreateTask,
  useLoadMoreByAssigneeGroup,
  useLoadMoreByStatus,
  useResolveComment,
  useUpdateTask,
} from "./mutations";
import {
  taskKeys,
  type TaskSortParam,
} from "./queries";
import { inboxKeys } from "../inbox/queries";
import { workflowKeys } from "../workflows/queries";
import type {
  GroupedTasksResponse,
  InboxItem,
  Task,
  ListTasksCache,
  ListTasksParams,
  ListGroupedTasksParams,
  ListTasksResponse,
  TimelineEntry,
} from "../types";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const WS_ID = "ws-1";

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
    project_id: null,
    position: idx,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
  properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeInboxItem(
  id: string,
  taskId: string,
  overrides: Partial<InboxItem> = {},
): InboxItem {
  return {
    id,
    workspace_id: WS_ID,
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: "member",
    actor_id: "user-2",
    type: "status_changed",
    severity: "info",
    task_id: taskId,
    title: `Inbox ${id}`,
    body: null,
    task_status: "todo",
    read: false,
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    details: null,
    ...overrides,
  };
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useCreateTask", () => {
  let qc: QueryClient;
  let createTask: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    createTask = vi.fn();
    setApiInstance({ createTask } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("invalidates the assigned SOP run so its Stage board shows the new Task", async () => {
    const instanceKey = workflowKeys.instance(WS_ID, "run-1");
    qc.setQueryData(instanceKey, {
      id: "run-1",
      tasks: [],
    });
    createTask.mockResolvedValue(
      makeTask(1, {
        project_id: "project-1",
        workflow_instance_id: "run-1",
        workflow_stage_id: "stage-1",
      }),
    );
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");
    const { result } = renderHook(() => useCreateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        title: "Project1",
        project_id: "project-1",
        workflow_instance_id: "run-1",
        workflow_stage_id: "stage-1",
      });
    });

    expect(
      invalidateSpy.mock.calls.some(
        ([filters]) =>
          JSON.stringify(filters?.queryKey) === JSON.stringify(instanceKey),
      ),
    ).toBe(true);
  });
});

describe("useLoadMoreByStatus", () => {
  let qc: QueryClient;
  let listTasks: ReturnType<typeof vi.fn<(p?: ListTasksParams) => Promise<ListTasksResponse>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listTasks = vi.fn();
    setApiInstance({ listTasks } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("targets the sorted cache key and forwards sort to the API", async () => {
    const sort: TaskSortParam = { sort_by: "priority", sort_direction: "desc" };
    const activeKey = taskKeys.listSorted(WS_ID, sort);
    const seed: ListTasksCache = {
      byStatus: {
        todo: { tasks: [makeTask(1)], total: 3 },
      },
    };
    qc.setQueryData<ListTasksCache>(activeKey, seed);

    listTasks.mockResolvedValue({
      tasks: [makeTask(2), makeTask(3)],
      total: 3,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", undefined, sort),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.hasMore).toBe(true);
    expect(result.current.total).toBe(3);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listTasks).toHaveBeenCalledWith({
      status: "todo",
      limit: 50,
      offset: 1,
      sort_by: "priority",
      sort_direction: "desc",
    });

    const updated = qc.getQueryData<ListTasksCache>(activeKey);
    expect(updated?.byStatus.todo?.tasks).toHaveLength(3);
    expect(updated?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      "task-1",
      "task-2",
      "task-3",
    ]);
  });

  it("ignores a stale cache entry under a different sort", async () => {
    // Stale entry from a previous sort lingers (kept by gcTime / keepPreviousData).
    const staleSort: TaskSortParam = { sort_by: "priority", sort_direction: "desc" };
    qc.setQueryData<ListTasksCache>(taskKeys.listSorted(WS_ID, staleSort), {
      byStatus: { todo: { tasks: [makeTask(99)], total: 99 } },
    });

    // The active sort cache has its own bucket — load-more must target THIS one.
    const activeSort: TaskSortParam = { sort_by: "position", sort_direction: undefined };
    const activeKey = taskKeys.listSorted(WS_ID, activeSort);
    qc.setQueryData<ListTasksCache>(activeKey, {
      byStatus: { todo: { tasks: [makeTask(1)], total: 2 } },
    });

    listTasks.mockResolvedValue({
      tasks: [makeTask(2)],
      total: 2,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", undefined, activeSort),
      { wrapper: createWrapper(qc) },
    );

    // total derives from the active key, not the stale one.
    expect(result.current.total).toBe(2);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listTasks).toHaveBeenCalledWith(
      expect.objectContaining({ offset: 1, sort_by: "position" }),
    );

    const active = qc.getQueryData<ListTasksCache>(activeKey);
    expect(active?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      "task-1",
      "task-2",
    ]);

    // Stale cache is untouched.
    const stale = qc.getQueryData<ListTasksCache>(taskKeys.listSorted(WS_ID, staleSort));
    expect(stale?.byStatus.todo?.tasks.map((i) => i.id)).toEqual(["task-99"]);
  });

  it("targets the myList scoped cache when myTasks is provided", async () => {
    const sort: TaskSortParam = { sort_by: "title", sort_direction: "asc" };
    const myTasks = { scope: "assigned", filter: { assignee_id: "user-1" } };
    const activeKey = taskKeys.myListSorted(WS_ID, myTasks.scope, myTasks.filter, sort);
    qc.setQueryData<ListTasksCache>(activeKey, {
      byStatus: { in_progress: { tasks: [makeTask(1, { status: "in_progress" })], total: 2 } },
    });

    listTasks.mockResolvedValue({
      tasks: [makeTask(2, { status: "in_progress" })],
      total: 2,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("in_progress", myTasks, sort),
      { wrapper: createWrapper(qc) },
    );

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listTasks).toHaveBeenCalledWith({
      status: "in_progress",
      limit: 50,
      offset: 1,
      sort_by: "title",
      sort_direction: "asc",
      assignee_id: "user-1",
    });

    const updated = qc.getQueryData<ListTasksCache>(activeKey);
    expect(updated?.byStatus.in_progress?.tasks).toHaveLength(2);
  });

  it("targets the project surface cache when myTasks uses a project scope key", async () => {
    const sort: TaskSortParam = { sort_by: "position", sort_direction: undefined };
    const myTasks = { scope: "project:p1", filter: { project_id: "p1" } };
    const activeKey = taskKeys.myListSorted(WS_ID, myTasks.scope, myTasks.filter, sort);
    qc.setQueryData<ListTasksCache>(activeKey, {
      byStatus: { todo: { tasks: [makeTask(1, { project_id: "p1" })], total: 2 } },
    });

    listTasks.mockResolvedValue({
      tasks: [makeTask(2, { project_id: "p1" })],
      total: 2,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", myTasks, sort),
      { wrapper: createWrapper(qc) },
    );

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listTasks).toHaveBeenCalledWith({
      status: "todo",
      limit: 50,
      offset: 1,
      sort_by: "position",
      sort_direction: undefined,
      project_id: "p1",
    });

    const updated = qc.getQueryData<ListTasksCache>(activeKey);
    expect(updated?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      "task-1",
      "task-2",
    ]);
  });

  it("works with no sort (matches the {} key used by sort-less callers)", async () => {
    const myTasks = { scope: "actor", filter: { assignee_id: "user-2" } };
    const activeKey = taskKeys.myListSorted(WS_ID, myTasks.scope, myTasks.filter, undefined);
    qc.setQueryData<ListTasksCache>(activeKey, {
      byStatus: { todo: { tasks: [makeTask(1)], total: 2 } },
    });

    listTasks.mockResolvedValue({ tasks: [makeTask(2)], total: 2 });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", myTasks),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.total).toBe(2);
    expect(result.current.hasMore).toBe(true);

    await act(async () => {
      await result.current.loadMore();
    });

    const updated = qc.getQueryData<ListTasksCache>(activeKey);
    expect(updated?.byStatus.todo?.tasks).toHaveLength(2);
  });
});

describe("useLoadMoreByAssigneeGroup", () => {
  let qc: QueryClient;
  let listGroupedTasks: ReturnType<
    typeof vi.fn<(p: ListGroupedTasksParams) => Promise<GroupedTasksResponse>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listGroupedTasks = vi.fn();
    setApiInstance({ listGroupedTasks } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("forwards sort to the grouped API and appends into the right group", async () => {
    const sort: TaskSortParam = { sort_by: "priority", sort_direction: "desc" };
    const queryKey = ["custom", "assignee-groups", "ws-1"] as const;
    const seed: GroupedTasksResponse = {
      groups: [
        {
          id: "assignee:member:user-1",
          assignee_type: "member",
          assignee_id: "user-1",
          tasks: [makeTask(1, { assignee_type: "member", assignee_id: "user-1" })],
          total: 2,
        },
      ],
    };
    qc.setQueryData<GroupedTasksResponse>(queryKey, seed);

    listGroupedTasks.mockResolvedValue({
      groups: [
        {
          id: "assignee:member:user-1",
          assignee_type: "member",
          assignee_id: "user-1",
          tasks: [makeTask(2, { assignee_type: "member", assignee_id: "user-1" })],
          total: 2,
        },
      ],
    });

    const { result } = renderHook(
      () =>
        useLoadMoreByAssigneeGroup(
          {
            id: "assignee:member:user-1",
            assignee_type: "member",
            assignee_id: "user-1",
          },
          queryKey,
          { statuses: ["todo"] },
          sort,
        ),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.hasMore).toBe(true);
    expect(result.current.total).toBe(2);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listGroupedTasks).toHaveBeenCalledWith({
      group_by: "assignee",
      limit: 50,
      offset: 1,
      sort_by: "priority",
      sort_direction: "desc",
      statuses: ["todo"],
      group_assignee_type: "member",
      group_assignee_id: "user-1",
    });

    const updated = qc.getQueryData<GroupedTasksResponse>(queryKey);
    expect(updated?.groups[0]?.tasks.map((i) => i.id)).toEqual([
      "task-1",
      "task-2",
    ]);
  });
});

describe("useUpdateTask — optimistic move keeps every bucketed board in sync", () => {
  const sort: TaskSortParam = { sort_by: "position", sort_direction: undefined };
  const myScope = "assigned";
  const myFilter = { assignee_id: "user-1" };
  const projectScope = "project:p1";
  const projectFilter = { project_id: "p1" };
  const wsKey = taskKeys.listSorted(WS_ID, sort);
  const inboxKey = inboxKeys.list(WS_ID);
  // My-Tasks AND the Project board both ride this myList cache; a move that
  // only patched the workspace cache snaps back on those boards.
  const myKey = taskKeys.myListSorted(WS_ID, myScope, myFilter, sort);
  const projectKey = taskKeys.myListSorted(WS_ID, projectScope, projectFilter, sort);

  let qc: QueryClient;
  let updateTask: ReturnType<typeof vi.fn<(id: string, data: unknown) => Promise<Task>>>;
  let moveTask: ReturnType<typeof vi.fn<(id: string, data: unknown) => Promise<Task>>>;

  function makeBucketed(): ListTasksCache {
    return {
      byStatus: {
        todo: { tasks: [makeTask(1)], total: 1 },
        in_progress: { tasks: [], total: 0 },
      },
    };
  }

  function bucketIds(
    key: readonly unknown[],
    status: "todo" | "in_progress",
  ): string[] {
    const c = qc.getQueryData<ListTasksCache>(key);
    return (c?.byStatus[status]?.tasks ?? []).map((i) => i.id);
  }

  function inboxStatus(taskId: string) {
    return qc
      .getQueryData<InboxItem[]>(inboxKey)
      ?.find((item) => item.task_id === taskId)?.task_status;
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    updateTask = vi.fn();
    moveTask = vi.fn();
    setApiInstance({ updateTask, moveTask } as unknown as ApiClient);
    qc.setQueryData<ListTasksCache>(wsKey, makeBucketed());
    qc.setQueryData<ListTasksCache>(myKey, makeBucketed());
    qc.setQueryData<ListTasksCache>(projectKey, makeBucketed());
    qc.setQueryData<InboxItem[]>(inboxKey, [
      makeInboxItem("inbox-1", "task-1"),
      makeInboxItem("inbox-2", "task-2"),
    ]);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically moves the card in both the workspace and myList caches", async () => {
    let resolve!: (task: Task) => void;
    updateTask.mockReturnValue(
      new Promise<Task>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "task-1", status: "in_progress", position: 5 });
    });

    // Optimistic state — the regression: myList must move too, not just ws.
    for (const key of [wsKey, myKey, projectKey]) {
      expect(bucketIds(key, "todo")).toEqual([]);
      expect(bucketIds(key, "in_progress")).toEqual(["task-1"]);
    }

    await act(async () => {
      resolve(makeTask(1, { status: "in_progress", position: 5 }));
    });

    // Authoritative settle keeps the card in place in both caches.
    for (const key of [wsKey, myKey, projectKey]) {
      expect(bucketIds(key, "in_progress")).toEqual(["task-1"]);
    }
  });

  it("uses server move intent while keeping provisional position optimistic-only", async () => {
    moveTask.mockResolvedValue(
      makeTask(1, { status: "in_progress", position: 15 }),
    );
    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        id: "task-1",
        status: "in_progress",
        position: 12,
        move_intent: {
          before_id: "task-0",
          after_id: "task-2",
        },
      });
    });

    expect(moveTask).toHaveBeenCalledWith("task-1", {
      status: "in_progress",
      before_id: "task-0",
      after_id: "task-2",
    });
    expect(updateTask).not.toHaveBeenCalled();
    expect(
      qc
        .getQueryData<ListTasksCache>(wsKey)
        ?.byStatus.in_progress?.tasks[0]?.position,
    ).toBe(15);
  });

  it("optimistically patches the linked inbox row status and reconciles with the server response", async () => {
    let resolve!: (task: Task) => void;
    updateTask.mockReturnValue(
      new Promise<Task>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "task-1", status: "in_progress" });
    });

    expect(inboxStatus("task-1")).toBe("in_progress");
    expect(inboxStatus("task-2")).toBe("todo");

    await act(async () => {
      resolve(makeTask(1, { status: "done" }));
    });

    expect(inboxStatus("task-1")).toBe("done");
    expect(inboxStatus("task-2")).toBe("todo");
  });

  it("retries a stale assignee update once against the latest revision", async () => {
    qc.setQueryData(
      taskKeys.detail(WS_ID, "task-1"),
      makeTask(1, { revision: "old-revision" }),
    );
    updateTask
      .mockRejectedValueOnce(
        new ApiError("conflict", 409, "Conflict", {
          code: "revision_conflict",
          current: { revision: "latest-revision" },
          attempted: {
            expected_revision: "old-revision",
            assignee_type: "agent",
            assignee_id: "agent-1",
          },
        }),
      )
      .mockResolvedValueOnce(
        makeTask(1, {
          revision: "saved-revision",
          assignee_type: "agent",
          assignee_id: "agent-1",
        }),
      );

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        id: "task-1",
        assignee_type: "agent",
        assignee_id: "agent-1",
      });
    });

    expect(updateTask).toHaveBeenNthCalledWith(1, "task-1", {
      assignee_type: "agent",
      assignee_id: "agent-1",
      expected_revision: "old-revision",
    });
    expect(updateTask).toHaveBeenNthCalledWith(2, "task-1", {
      assignee_type: "agent",
      assignee_id: "agent-1",
      expected_revision: "latest-revision",
    });
  });

  it("keeps revision protection when a repeated conflict returns the same revision", async () => {
    const current = makeTask(1, { revision: "2026-11-06T15:55:29Z" });
    qc.setQueryData(taskKeys.detail(WS_ID, current.id), current);
    updateTask.mockRejectedValue(new ApiError("conflict", 409, "Conflict", {
      code: "revision_conflict", current,
      attempted: { status: "done", expected_revision: current.revision },
    }));
    const { result } = renderHook(() => useUpdateTask(), { wrapper: createWrapper(qc) });
    await act(async () => {
      await expect(result.current.mutateAsync({ id: current.id, status: "done" })).rejects.toThrow();
    });
    expect(updateTask).toHaveBeenCalledTimes(2);
    for (const [, payload] of updateTask.mock.calls) {
      expect(payload).toMatchObject({ expected_revision: current.revision });
    }
  });

  it("requires review for text revision conflicts", async () => {
    const current = makeTask(1, { revision: "2026-11-06T15:55:29Z" });
    qc.setQueryData(taskKeys.detail(WS_ID, current.id), current);
    updateTask.mockRejectedValue(new ApiError("conflict", 409, "Conflict", {
      code: "revision_conflict", current,
      attempted: { description: "new text", expected_revision: current.revision },
    }));
    const { result } = renderHook(() => useUpdateTask(), { wrapper: createWrapper(qc) });
    await act(async () => { await expect(result.current.mutateAsync({ id: current.id, description: "new text" })).rejects.toThrow(); });
    expect(updateTask).toHaveBeenCalledTimes(1);
  });

  it("rolls both caches back when the request fails", async () => {
    updateTask.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ id: "task-1", status: "in_progress", position: 5 })
        .catch(() => {});
    });

    for (const key of [wsKey, myKey, projectKey]) {
      expect(bucketIds(key, "todo")).toEqual(["task-1"]);
      expect(bucketIds(key, "in_progress")).toEqual([]);
    }
  });

  it("rolls the linked inbox row status back when the request fails", async () => {
    updateTask.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ id: "task-1", status: "in_progress" })
        .catch(() => {});
    });

    expect(inboxStatus("task-1")).toBe("todo");
    expect(inboxStatus("task-2")).toBe("todo");
  });

  it("does not invalidate the board list on settle (no refetch flicker)", async () => {
    updateTask.mockResolvedValue(makeTask(1, { status: "in_progress", position: 5 }));
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ id: "task-1", status: "in_progress", position: 5 });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    // The board list + myList are reconciled surgically, never refetched.
    expect(invalidatedKeys).not.toContainEqual(taskKeys.list(WS_ID));
    expect(invalidatedKeys).not.toContainEqual(taskKeys.myAll(WS_ID));
  });

  it("surgically removes the task from the old project's list on a project move (no blanket myAll refetch)", async () => {
    // A project move makes the task leave the old project's filtered list.
    // The membership-aware coordinator removes the card from that loaded list
    // in onMutate — deterministic, no WS echo or refetch needed — replacing
    // the old blanket "invalidate myAll on settle" safety net (SILI-3669 /
    // #4548). Lists whose filter the move cannot affect stay untouched.
    let resolve!: (task: Task) => void;
    updateTask.mockReturnValue(
      new Promise<Task>((r) => {
        resolve = r;
      }),
    );
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "task-1", project_id: "project-9" });
    });

    // Optimistic: gone from the old project's list immediately; the
    // workspace board and the assignee-filtered list keep the card.
    expect(bucketIds(projectKey, "todo")).toEqual([]);
    expect(bucketIds(wsKey, "todo")).toEqual(["task-1"]);
    expect(bucketIds(myKey, "todo")).toEqual(["task-1"]);

    await act(async () => {
      resolve(makeTask(1, { project_id: "project-9" }));
    });

    expect(bucketIds(projectKey, "todo")).toEqual([]);
    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).not.toContainEqual(taskKeys.myAll(WS_ID));
  });

  it("rolls the membership removal back when a project move fails", async () => {
    updateTask.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ id: "task-1", project_id: "project-9" })
        .catch(() => {});
    });

    expect(bucketIds(projectKey, "todo")).toEqual(["task-1"]);
  });
});

describe("useUpdateTask — detaching a sub-task prunes the old parent's children cache", () => {
  const PARENT_ID = "parent-1";
  const childKey = taskKeys.children(WS_ID, PARENT_ID);

  let qc: QueryClient;
  let updateTask: ReturnType<typeof vi.fn<(id: string, data: unknown) => Promise<Task>>>;

  function childIds(): string[] {
    return (qc.getQueryData<Task[]>(childKey) ?? []).map((c) => c.id);
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    updateTask = vi.fn();
    setApiInstance({ updateTask } as unknown as ApiClient);
    // Seed the detail cache so onMutate resolves the old parent from the
    // freshest source, plus the parent's children list rendered by the
    // sub-tasks section.
    const child = makeTask(1, { parent_task_id: PARENT_ID, stage: 2 });
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, child.id), child);
    qc.setQueryData<Task[]>(childKey, [
      child,
      makeTask(2, { parent_task_id: PARENT_ID }),
    ]);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically removes the task from the old parent's children array", async () => {
    let resolve!: (task: Task) => void;
    updateTask.mockReturnValue(
      new Promise<Task>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "task-1", parent_task_id: null, stage: null });
    });

    // Pruned immediately so the parent's sub-tasks list drops it now, not
    // after the settle refetch; the sibling is untouched.
    expect(childIds()).toEqual(["task-2"]);

    await act(async () => {
      resolve(makeTask(1, { parent_task_id: null, stage: null }));
    });
    expect(childIds()).not.toContain("task-1");
  });

  it("restores the old parent's children when the request fails", async () => {
    updateTask.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ id: "task-1", parent_task_id: null, stage: null })
        .catch(() => {});
    });

    expect(childIds()).toEqual(["task-1", "task-2"]);
  });

  it("keeps the task under its parent for a non-reparenting update", async () => {
    updateTask.mockResolvedValue(
      makeTask(1, { parent_task_id: PARENT_ID, status: "done" }),
    );

    const { result } = renderHook(() => useUpdateTask(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "task-1", status: "done" });
    });

    // A status-only change patches in place — never prunes the relationship.
    expect(childIds()).toEqual(["task-1", "task-2"]);
  });
});

describe("useBatchUpdateTasks — optimistic patch covers filtered boards too", () => {
  const sort: TaskSortParam = { sort_by: "position", sort_direction: undefined };
  const myScope = "assigned";
  const myFilter = { assignee_id: "user-1" };
  const wsKey = taskKeys.listSorted(WS_ID, sort);
  const myKey = taskKeys.myListSorted(WS_ID, myScope, myFilter, sort);

  let qc: QueryClient;
  let batchUpdateTasks: ReturnType<
    typeof vi.fn<(ids: string[], updates: unknown) => Promise<{ updated: number }>>
  >;

  function makeBucketed(): ListTasksCache {
    return {
      byStatus: {
        todo: { tasks: [makeTask(1)], total: 1 },
        in_progress: { tasks: [], total: 0 },
      },
    };
  }

  function bucketIds(key: readonly unknown[], status: "todo" | "in_progress"): string[] {
    const c = qc.getQueryData<ListTasksCache>(key);
    return (c?.byStatus[status]?.tasks ?? []).map((i) => i.id);
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    batchUpdateTasks = vi.fn();
    setApiInstance({ batchUpdateTasks } as unknown as ApiClient);
    qc.setQueryData<ListTasksCache>(wsKey, makeBucketed());
    qc.setQueryData<ListTasksCache>(myKey, makeBucketed());
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically patches BOTH the workspace and myList caches (not just ws)", async () => {
    let resolve!: (r: { updated: number }) => void;
    batchUpdateTasks.mockReturnValue(
      new Promise<{ updated: number }>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ ids: ["task-1"], updates: { status: "in_progress" } });
    });

    // The regression Howard flagged: batch must move the card on the myList
    // board too, not only the workspace board. onMutate awaits cancelQueries,
    // so the optimistic patch lands a microtask later — wait for it.
    await waitFor(() => {
      for (const key of [wsKey, myKey]) {
        expect(bucketIds(key, "todo")).toEqual([]);
        expect(bucketIds(key, "in_progress")).toEqual(["task-1"]);
      }
    });

    await act(async () => {
      resolve({ updated: 1 });
    });

    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "in_progress")).toEqual(["task-1"]);
    }
  });

  it("rolls both caches back when the request fails", async () => {
    batchUpdateTasks.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ ids: ["task-1"], updates: { status: "in_progress" } })
        .catch(() => {});
    });

    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "todo")).toEqual(["task-1"]);
      expect(bucketIds(key, "in_progress")).toEqual([]);
    }
  });

  it("does not invalidate the board list on settle (no refetch flicker)", async () => {
    batchUpdateTasks.mockResolvedValue({ updated: 1 });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ ids: ["task-1"], updates: { status: "in_progress" } });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).not.toContainEqual(taskKeys.list(WS_ID));
  });

  it("surgically removes moved tasks from the old project's list (no blanket myAll refetch)", async () => {
    // Mirrors useUpdateTask: a batch project move drops the cards from the
    // old project's loaded list via the membership-aware coordinator instead
    // of refetching every filtered list (SILI-3669 / #4548).
    const projectScope = "project:p1";
    const projectFilter = { project_id: "p1" };
    const projectKey = taskKeys.myListSorted(WS_ID, projectScope, projectFilter, sort);
    qc.setQueryData<ListTasksCache>(projectKey, makeBucketed());
    batchUpdateTasks.mockResolvedValue({ updated: 1 });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useBatchUpdateTasks(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        ids: ["task-1"],
        updates: { project_id: "project-9" },
      });
    });

    expect(bucketIds(projectKey, "todo")).toEqual([]);
    // The assignee-filtered list is untouched by a project move.
    expect(bucketIds(myKey, "todo")).toEqual(["task-1"]);
    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).not.toContainEqual(taskKeys.myAll(WS_ID));
  });
});

describe("useResolveComment", () => {
  const TASK_ID = "task-1";

  function makeComment(
    id: string,
    parentId: string | null,
    resolvedAt: string | null,
  ): TimelineEntry {
    return {
      type: "comment",
      id,
      actor_type: "member",
      actor_id: "user-1",
      content: id,
      parent_id: parentId,
      comment_type: "comment",
      reactions: [],
      attachments: [],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      resolved_at: resolvedAt,
      resolved_by_type: resolvedAt ? "member" : null,
      resolved_by_id: resolvedAt ? "user-1" : null,
    };
  }

  // Two independent threads on one task:
  //   root1 ─ a1 (resolved), b1
  //   root2 ─ a2 (resolved)
  function seedTimeline(qc: QueryClient) {
    const entries: TimelineEntry[] = [
      makeComment("root1", null, null),
      makeComment("a1", "root1", "2026-01-01T00:01:00Z"),
      makeComment("b1", "root1", null),
      makeComment("root2", null, null),
      makeComment("a2", "root2", "2026-01-01T00:05:00Z"),
    ];
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(TASK_ID), entries);
  }

  function resolvedIds(qc: QueryClient): string[] {
    const cache = qc.getQueryData<TimelineEntry[]>(taskKeys.timeline(TASK_ID)) ?? [];
    return cache.filter((e) => e.resolved_at).map((e) => e.id).sort();
  }

  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    setApiInstance({
      resolveComment: vi.fn().mockResolvedValue({ id: "b1" }),
      unresolveComment: vi.fn().mockResolvedValue({ id: "b1" }),
    } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("clears the prior resolution in the same thread when resolving another comment", async () => {
    seedTimeline(qc);

    const { result } = renderHook(() => useResolveComment(TASK_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ commentId: "b1", resolved: true });
    });

    // b1 replaces a1 inside thread 1; a2 (thread 2) is untouched.
    expect(resolvedIds(qc)).toEqual(["a2", "b1"]);
  });

  it("does not clear resolutions in other threads", async () => {
    seedTimeline(qc);

    const { result } = renderHook(() => useResolveComment(TASK_ID), {
      wrapper: createWrapper(qc),
    });

    // Resolving root1 (thread 1) must leave a2 (thread 2) resolved.
    await act(async () => {
      await result.current.mutateAsync({ commentId: "root1", resolved: true });
    });

    expect(resolvedIds(qc)).toEqual(["a2", "root1"]);
  });

  it("unresolve only clears its own row, never siblings", async () => {
    // Legacy state: two resolved comments coexist in one thread.
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(TASK_ID), [
      makeComment("root1", null, null),
      makeComment("a1", "root1", "2026-01-01T00:01:00Z"),
      makeComment("b1", "root1", "2026-01-01T00:02:00Z"),
    ]);

    const { result } = renderHook(() => useResolveComment(TASK_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ commentId: "b1", resolved: false });
    });

    // Only b1 is cleared; a1 stays resolved (unresolve never mirrors the clear).
    expect(resolvedIds(qc)).toEqual(["a1"]);
  });
});
