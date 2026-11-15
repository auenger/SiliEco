import { agentRunsKeys } from "@silieco/core/agent-runs/queries";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  QueryObserver,
} from "@tanstack/react-query";
import { setApiInstance } from "@silieco/core/api";
import { agentRunSnapshotKeys } from "@silieco/core/agents/queries";
import { useBatchDeleteTasks, useDeleteTask } from "@silieco/core/tasks/mutations";
import { taskKeys } from "@silieco/core/tasks/queries";
import { labelKeys } from "@silieco/core/labels/queries";
import { WorkspaceSlugProvider } from "@silieco/core/paths";
import { workspaceKeys } from "@silieco/core/workspace/queries";
import type {
  AgentRun,
  Attachment,
  Task,
  TaskLabelsResponse,
  TaskUsageSummary,
  Label,
  ListTasksCache,
  TimelineEntry,
  Workspace,
} from "@silieco/core/types";

const WS_ID = "ws-1";
const SLUG = "test";
const TASK_ID = "task-1";
const OTHER_TASK_ID = "task-2";
const PARENT_TASK_ID = "parent-1";
const AGENT_ID = "agent-1";

const workspace: Workspace = {
  id: WS_ID,
  name: "Test",
  slug: SLUG,
  description: null,
  context: null,
  settings: {},
  repos: [],
  task_prefix: "TST",
  avatar_url: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const baseTask: Task = {
  id: TASK_ID,
  workspace_id: WS_ID,
  number: 1,
  identifier: "TST-1",
  title: "Deleted task",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_task_id: PARENT_TASK_ID,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  labels: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const otherTask: Task = {
  ...baseTask,
  id: OTHER_TASK_ID,
  number: 2,
  identifier: "TST-2",
  title: "Other task",
  parent_task_id: null,
};

const usage: TaskUsageSummary = {
  total_input_tokens: 10,
  total_output_tokens: 20,
  total_cache_read_tokens: 1,
  total_cache_write_tokens: 2,
  run_count: 1,
};

const attachment: Attachment = {
  id: "attachment-1",
  workspace_id: WS_ID,
  task_id: TASK_ID,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "member",
  uploader_id: "member-1",
  filename: "evidence.png",
  url: "s3://bucket/evidence.png",
  download_url: "https://example.test/evidence.png",
  markdown_url: "https://example.test/api/attachments/attachment-1/download",
  content_type: "image/png",
  size_bytes: 1,
  created_at: "2026-01-01T00:00:00Z",
};

const timeline: TimelineEntry[] = [
  {
    type: "activity",
    id: "activity-1",
    actor_type: "member",
    actor_id: "member-1",
    action: "created",
    created_at: "2026-01-01T00:00:00Z",
  },
];

const label: Label = {
  id: "label-1",
  workspace_id: WS_ID,
  name: "bug",
  color: "#ef4444",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const taskLabels: TaskLabelsResponse = {
  labels: [label],
};

function makeListCache(...tasks: Task[]): ListTasksCache {
  return {
    byStatus: {
      todo: { tasks, total: tasks.length },
    },
  };
}

function makeTask(taskId = TASK_ID): AgentRun {
  return {
    id: `task-${taskId}`,
    agent_id: AGENT_ID,
    runtime_id: "runtime-1",
    task_id: taskId,
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-01-01T00:00:00Z",
    completed_at: "2026-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function setup(
  deleteTask = vi.fn().mockResolvedValue(undefined),
  batchDeleteTasks = vi.fn().mockResolvedValue({ deleted: 0 }),
) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  qc.setQueryData(workspaceKeys.list(), [workspace]);
  setApiInstance({
    listWorkspaces: vi.fn().mockResolvedValue([workspace]),
    deleteTask,
    batchDeleteTasks,
  } as any);

  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug={SLUG}>{children}</WorkspaceSlugProvider>
    </QueryClientProvider>
  );

  return { qc, deleteTask, batchDeleteTasks, wrapper };
}

function ids(cache: ListTasksCache | undefined) {
  return cache?.byStatus.todo?.tasks.map((task) => task.id);
}

function expectInvalidated(qc: QueryClient, queryKey: readonly unknown[]) {
  expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
}

describe("useDeleteTask", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("cleans list, my-list, task-scoped, and dependent agent caches after a successful single delete", async () => {
    const { qc, deleteTask, wrapper } = setup();
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    qc.setQueryData<ListTasksCache>(
      taskKeys.list(WS_ID),
      makeListCache(baseTask, otherTask),
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", assignedFilter),
      makeListCache(baseTask, otherTask),
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "created", createdFilter),
      makeListCache(baseTask),
    );
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      [baseTask, otherTask],
    );
    qc.setQueryData<TaskUsageSummary>(taskKeys.usage(TASK_ID), usage);
    qc.setQueryData<AgentRun[]>(agentRunSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);

    const { result } = renderHook(() => useDeleteTask(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(TASK_ID);
    });

    expect(deleteTask).toHaveBeenCalledWith(TASK_ID);
    expect(ids(qc.getQueryData(taskKeys.list(WS_ID)))).toEqual([
      OTHER_TASK_ID,
    ]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([OTHER_TASK_ID]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.usage(TASK_ID))).toBeUndefined();
    expect(
      qc
        .getQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID))
        ?.map((task) => task.id),
    ).toEqual([OTHER_TASK_ID]);
    expectInvalidated(qc, agentRunSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentRunsKeys.detail(WS_ID, AGENT_ID));
  });

  it("optimistically prunes parent children without invalidating them before delete settles", async () => {
    const pendingDelete = deferred<void>();
    const { qc, wrapper } = setup(vi.fn(() => pendingDelete.promise));
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      [baseTask, otherTask],
    );

    const { result } = renderHook(() => useDeleteTask(), { wrapper });
    let mutation!: Promise<void>;

    await act(async () => {
      mutation = result.current.mutateAsync(TASK_ID);
      await Promise.resolve();
    });

    expect(
      qc
        .getQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID))
        ?.map((task) => task.id),
    ).toEqual([OTHER_TASK_ID]);
    expect(
      qc.getQueryState(taskKeys.children(WS_ID, PARENT_TASK_ID))
        ?.isInvalidated,
    ).not.toBe(true);

    await act(async () => {
      pendingDelete.resolve();
      await mutation;
    });

    expectInvalidated(qc, taskKeys.children(WS_ID, PARENT_TASK_ID));
  });

  it("invalidates child progress when a single delete removes a parent task", async () => {
    const { qc, wrapper } = setup();
    const parentTask = { ...baseTask, parent_task_id: null };
    const childTask = { ...otherTask, parent_task_id: TASK_ID };
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), parentTask);
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, TASK_ID), [
      childTask,
    ]);
    qc.setQueryData(
      taskKeys.childProgress(WS_ID),
      new Map([[TASK_ID, { done: 0, total: 1 }]]),
    );

    const { result } = renderHook(() => useDeleteTask(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(TASK_ID);
    });

    expect(qc.getQueryData(taskKeys.children(WS_ID, TASK_ID))).toBeUndefined();
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("restores optimistic snapshots when a single delete fails", async () => {
    const error = new Error("delete failed");
    const { qc, wrapper } = setup(vi.fn().mockRejectedValue(error));
    const assignedFilter = { assignee_id: AGENT_ID };
    const list = makeListCache(baseTask, otherTask);
    const myList = makeListCache(baseTask);
    const children = [baseTask, otherTask];
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), list);
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", assignedFilter),
      myList,
    );
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      children,
    );
    qc.setQueryData<TaskUsageSummary>(taskKeys.usage(TASK_ID), usage);

    const { result } = renderHook(() => useDeleteTask(), { wrapper });

    await expect(
      act(async () => {
        await result.current.mutateAsync(TASK_ID);
      }),
    ).rejects.toThrow("delete failed");

    expect(qc.getQueryData(taskKeys.list(WS_ID))).toEqual(list);
    expect(
      qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter)),
    ).toEqual(myList);
    expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toEqual(
      baseTask,
    );
    expect(qc.getQueryData(taskKeys.children(WS_ID, PARENT_TASK_ID))).toEqual(
      children,
    );
    expect(qc.getQueryData(taskKeys.usage(TASK_ID))).toEqual(usage);
  });
});

describe("useBatchDeleteTasks", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("cleans list, my-list, task-scoped, parent, and dependent agent caches after a fully successful batch delete", async () => {
    const batchDeleteTasks = vi.fn().mockResolvedValue({ deleted: 2 });
    const { qc, wrapper } = setup(undefined, batchDeleteTasks);
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const idsToDelete = [TASK_ID, OTHER_TASK_ID];
    const childTask = { ...otherTask, parent_task_id: PARENT_TASK_ID };
    qc.setQueryData<ListTasksCache>(
      taskKeys.list(WS_ID),
      makeListCache(baseTask, childTask),
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", assignedFilter),
      makeListCache(baseTask, childTask),
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "created", createdFilter),
      makeListCache(baseTask),
    );
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, OTHER_TASK_ID), childTask);
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      [baseTask, childTask],
    );
    qc.setQueryData(taskKeys.childProgress(WS_ID), {});
    qc.setQueryData<TaskUsageSummary>(taskKeys.usage(TASK_ID), usage);
    qc.setQueryData<TaskUsageSummary>(
      taskKeys.usage(OTHER_TASK_ID),
      usage,
    );
    qc.setQueryData<AgentRun[]>(agentRunsKeys.forTask(WS_ID, TASK_ID), [makeTask()]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.forTask(WS_ID, OTHER_TASK_ID), [
      makeTask(OTHER_TASK_ID),
    ]);
    qc.setQueryData<AgentRun[]>(agentRunSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);

    const { result } = renderHook(() => useBatchDeleteTasks(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync(idsToDelete);
    });

    expect(batchDeleteTasks).toHaveBeenCalledWith(idsToDelete);
    expect(ids(qc.getQueryData(taskKeys.list(WS_ID)))).toEqual([]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toBeUndefined();
    expect(
      qc.getQueryData(taskKeys.detail(WS_ID, OTHER_TASK_ID)),
    ).toBeUndefined();
    expect(qc.getQueryData(taskKeys.usage(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.usage(OTHER_TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(agentRunsKeys.forTask(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(agentRunsKeys.forTask(WS_ID, OTHER_TASK_ID))).toBeUndefined();
    expect(
      qc.getQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID)),
    ).toEqual([]);
    expectInvalidated(qc, taskKeys.children(WS_ID, PARENT_TASK_ID));
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
    expectInvalidated(qc, agentRunSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentRunsKeys.detail(WS_ID, AGENT_ID));
  });

  it("invalidates child progress when a full batch delete removes a parent task", async () => {
    const batchDeleteTasks = vi.fn().mockResolvedValue({ deleted: 1 });
    const { qc, wrapper } = setup(undefined, batchDeleteTasks);
    const parentTask = { ...baseTask, parent_task_id: null };
    const childTask = { ...otherTask, parent_task_id: TASK_ID };
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), parentTask);
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, TASK_ID), [
      childTask,
    ]);
    qc.setQueryData(
      taskKeys.childProgress(WS_ID),
      new Map([[TASK_ID, { done: 0, total: 1 }]]),
    );

    const { result } = renderHook(() => useBatchDeleteTasks(), { wrapper });

    await act(async () => {
      await result.current.mutateAsync([TASK_ID]);
    });

    expect(batchDeleteTasks).toHaveBeenCalledWith([TASK_ID]);
    expect(qc.getQueryData(taskKeys.children(WS_ID, TASK_ID))).toBeUndefined();
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("restores optimistic list snapshots on partial batch delete before invalidating caches", async () => {
    const batchDeleteTasks = vi.fn().mockResolvedValue({ deleted: 1 });
    const { qc, wrapper } = setup(undefined, batchDeleteTasks);
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const idsToDelete = [TASK_ID, OTHER_TASK_ID];
    const childTask = { ...otherTask, parent_task_id: PARENT_TASK_ID };
    const list = makeListCache(baseTask, childTask);
    const assignedMyList = makeListCache(baseTask, childTask);
    const createdMyList = makeListCache(baseTask);
    const children = [baseTask, childTask];
    qc.setQueryData<ListTasksCache>(
      taskKeys.list(WS_ID),
      list,
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", assignedFilter),
      assignedMyList,
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "created", createdFilter),
      createdMyList,
    );
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      children,
    );
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, OTHER_TASK_ID), childTask);
    qc.setQueryData<TaskUsageSummary>(taskKeys.usage(TASK_ID), usage);
    qc.setQueryData<Attachment[]>(taskKeys.attachments(TASK_ID), [
      attachment,
    ]);
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(TASK_ID), timeline);
    qc.setQueryData<TaskLabelsResponse>(
      labelKeys.byTask(WS_ID, TASK_ID),
      taskLabels,
    );
    qc.setQueryData<TaskUsageSummary>(
      taskKeys.usage(OTHER_TASK_ID),
      usage,
    );
    qc.setQueryData<Attachment[]>(taskKeys.attachments(OTHER_TASK_ID), [
      { ...attachment, id: "attachment-2", task_id: OTHER_TASK_ID },
    ]);
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(OTHER_TASK_ID), [
      { ...timeline[0]!, id: "activity-2" },
    ]);
    qc.setQueryData<TaskLabelsResponse>(
      labelKeys.byTask(WS_ID, OTHER_TASK_ID),
      taskLabels,
    );
    qc.setQueryData<AgentRun[]>(agentRunSnapshotKeys.list(WS_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);
    const listRefetch = deferred<ListTasksCache>();
    const listObserver = new QueryObserver(qc, {
      queryKey: taskKeys.list(WS_ID),
      queryFn: () => listRefetch.promise,
      refetchOnMount: false,
      staleTime: Infinity,
    });
    const unsubscribeList = listObserver.subscribe(() => {});

    const { result } = renderHook(() => useBatchDeleteTasks(), { wrapper });

    try {
      await act(async () => {
        await result.current.mutateAsync(idsToDelete);
      });

      expect(batchDeleteTasks).toHaveBeenCalledWith(idsToDelete);
      expect(qc.getQueryData(taskKeys.list(WS_ID))).toEqual(list);
      expect(
        qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter)),
      ).toEqual(assignedMyList);
      expect(
        qc.getQueryData(taskKeys.myList(WS_ID, "created", createdFilter)),
      ).toEqual(createdMyList);
      expect(
        qc.getQueryData(taskKeys.children(WS_ID, PARENT_TASK_ID)),
      ).toEqual(children);
      expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toEqual(
        baseTask,
      );
      expect(qc.getQueryData(taskKeys.detail(WS_ID, OTHER_TASK_ID))).toEqual(
        childTask,
      );
      expect(qc.getQueryData(taskKeys.usage(TASK_ID))).toEqual(usage);
      expect(qc.getQueryData(taskKeys.attachments(TASK_ID))).toEqual([
        attachment,
      ]);
      expect(qc.getQueryData(taskKeys.timeline(TASK_ID))).toEqual(timeline);
      expect(qc.getQueryData(labelKeys.byTask(WS_ID, TASK_ID))).toEqual(
        taskLabels,
      );
      expect(qc.getQueryData(taskKeys.usage(OTHER_TASK_ID))).toEqual(usage);
      expect(qc.getQueryData(taskKeys.attachments(OTHER_TASK_ID))).toEqual([
        { ...attachment, id: "attachment-2", task_id: OTHER_TASK_ID },
      ]);
      expect(qc.getQueryData(taskKeys.timeline(OTHER_TASK_ID))).toEqual([
        { ...timeline[0]!, id: "activity-2" },
      ]);
      expect(qc.getQueryData(labelKeys.byTask(WS_ID, OTHER_TASK_ID))).toEqual(
        taskLabels,
      );
      expectInvalidated(qc, taskKeys.detail(WS_ID, TASK_ID));
      expectInvalidated(qc, taskKeys.detail(WS_ID, OTHER_TASK_ID));
      expectInvalidated(qc, taskKeys.usage(TASK_ID));
      expectInvalidated(qc, taskKeys.attachments(TASK_ID));
      expectInvalidated(qc, taskKeys.timeline(TASK_ID));
      expectInvalidated(qc, labelKeys.byTask(WS_ID, TASK_ID));
      expectInvalidated(qc, taskKeys.usage(OTHER_TASK_ID));
      expectInvalidated(qc, taskKeys.attachments(OTHER_TASK_ID));
      expectInvalidated(qc, taskKeys.timeline(OTHER_TASK_ID));
      expectInvalidated(qc, labelKeys.byTask(WS_ID, OTHER_TASK_ID));
      expectInvalidated(qc, agentRunSnapshotKeys.list(WS_ID));
      expectInvalidated(qc, agentRunsKeys.detail(WS_ID, AGENT_ID));
    } finally {
      unsubscribeList();
      listRefetch.resolve(list);
    }
  });

  it("restores optimistic workspace, my-list, and parent children snapshots when a batch delete fails", async () => {
    const pendingDelete = deferred<{ deleted: number }>();
    const { qc, wrapper } = setup(undefined, vi.fn(() => pendingDelete.promise));
    const assignedFilter = { assignee_id: AGENT_ID };
    const createdFilter = { creator_id: "member-1" };
    const list = makeListCache(baseTask, otherTask);
    const assignedMyList = makeListCache(baseTask, otherTask);
    const createdMyList = makeListCache(baseTask);
    const children = [baseTask, otherTask];
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), list);
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", assignedFilter),
      assignedMyList,
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "created", createdFilter),
      createdMyList,
    );
    qc.setQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
      children,
    );

    const { result } = renderHook(() => useBatchDeleteTasks(), { wrapper });
    let mutation!: Promise<{ deleted: number }>;

    await act(async () => {
      mutation = result.current.mutateAsync([TASK_ID]);
      await Promise.resolve();
    });

    expect(ids(qc.getQueryData(taskKeys.list(WS_ID)))).toEqual([
      OTHER_TASK_ID,
    ]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter))),
    ).toEqual([OTHER_TASK_ID]);
    expect(
      ids(qc.getQueryData(taskKeys.myList(WS_ID, "created", createdFilter))),
    ).toEqual([]);
    expect(
      qc
        .getQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID))
        ?.map((task) => task.id),
    ).toEqual([OTHER_TASK_ID]);

    await expect(
      act(async () => {
        pendingDelete.reject(new Error("batch delete failed"));
        await mutation;
      }),
    ).rejects.toThrow("batch delete failed");

    expect(qc.getQueryData(taskKeys.list(WS_ID))).toEqual(list);
    expect(
      qc.getQueryData(taskKeys.myList(WS_ID, "assigned", assignedFilter)),
    ).toEqual(assignedMyList);
    expect(
      qc.getQueryData(taskKeys.myList(WS_ID, "created", createdFilter)),
    ).toEqual(createdMyList);
    expect(qc.getQueryData(taskKeys.children(WS_ID, PARENT_TASK_ID))).toEqual(
      children,
    );
  });
});
