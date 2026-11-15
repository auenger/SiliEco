/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@silieco/core/api";
import type { ApiClient } from "@silieco/core/api/client";
import {
  getTaskSurfaceViewStore,
  pruneTaskSurfaceViewStates,
} from "@silieco/core/tasks/stores/surface-view-store";
import { ViewStoreProvider } from "@silieco/core/tasks/stores/view-store-context";
import type {
  AgentRun,
  Task,
  TaskStatus,
  ListTasksParams,
  ListTasksResponse,
  WorkspaceWorkingAgent,
} from "@silieco/core/types";
import { useTaskSurfaceController } from "./use-task-surface-controller";
import { TaskTableExportIntegrityError } from "../components/table-view-model";
import { statusTableMethodsFromLegacy } from "./status-table-test-api";

function makeTask(
  overrides: Partial<Task> & Pick<Task, "id" | "status">,
): Task {
  return {
    workspace_id: "ws-1",
    number: 1,
    identifier: "SILI-1",
    title: overrides.id,
    description: null,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: "p1",
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const updateTaskMutate = vi.hoisted(() => vi.fn());
const batchUpdateMutateAsync = vi.hoisted(() => vi.fn());
const batchDeleteMutateAsync = vi.hoisted(() => vi.fn());
const openModal = vi.hoisted(() => vi.fn());

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@silieco/core/tasks/mutations", () => ({
  useUpdateTask: () => ({ mutate: updateTaskMutate, isPending: false }),
  useBatchUpdateTasks: () => ({
    mutateAsync: batchUpdateMutateAsync,
    isPending: false,
  }),
  useBatchDeleteTasks: () => ({
    mutateAsync: batchDeleteMutateAsync,
    isPending: false,
  }),
}));

vi.mock("@silieco/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "translated" }),
}));

function makeWrapper(qc: QueryClient, surfaceKey = "project:p1") {
  const store = getTaskSurfaceViewStore(surfaceKey);
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
      </QueryClientProvider>
    );
  };
}

function never<T>() {
  return new Promise<T>(() => {});
}

function makeRunningTask(id: string, agentId: string, taskId: string): AgentRun {
  return {
    id,
    agent_id: agentId,
    runtime_id: "runtime-1",
    task_id: taskId,
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-01-01T00:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

function makeWorkingAgent(
  id: string,
  taskIDs: string[] = [],
  runningTaskCount = 1,
): WorkspaceWorkingAgent {
  return {
    id,
    name: id,
    avatar_url: null,
    running_run_count: runningTaskCount,
    task_ids: taskIDs,
  };
}

describe("useTaskSurfaceController", () => {
  let qc: QueryClient;
  let listTasks: ReturnType<
    typeof vi.fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>
  >;
  let getAgentRunSnapshot: ReturnType<
    typeof vi.fn<() => Promise<AgentRun[]>>
  >;
  let getWorkspaceWorkingAgents: ReturnType<
    typeof vi.fn<() => Promise<WorkspaceWorkingAgent[]>>
  >;
  let listTaskTableRows: ReturnType<typeof vi.fn>;
  let listTaskTableFacets: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listTasks = vi.fn(() => never<ListTasksResponse>());
    getAgentRunSnapshot = vi.fn(() => never<AgentRun[]>());
    getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([] satisfies WorkspaceWorkingAgent[]),
    );
    const tableMethods = statusTableMethodsFromLegacy(listTasks);
    listTaskTableRows = vi.fn(tableMethods.listTaskTableRows);
    listTaskTableFacets = vi.fn(tableMethods.listTaskTableFacets);
    setApiInstance({
      listTasks,
      ...tableMethods,
      listTaskTableRows,
      listTaskTableFacets,
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot,
      getWorkspaceWorkingAgents,
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);
    pruneTaskSurfaceViewStates([]);
    updateTaskMutate.mockClear();
    openModal.mockClear();
    batchUpdateMutateAsync.mockResolvedValue(undefined);
    batchDeleteMutateAsync.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneTaskSurfaceViewStates([]);
    vi.restoreAllMocks();
  });

  it("derives the project scope and canonical server query", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setSortBy("priority");
    store.getState().setSortDirection("desc");

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalled());

    const expectedSort = { sort_by: "priority", sort_direction: "desc" } as const;
    const expectedFilter = { project_id: "p1" };

    expect(result.current.scopeKey).toBe("project:p1");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.sort).toEqual(expectedSort);
    expect(result.current.tableQuerySpec).toEqual(
      expect.objectContaining({
        scope: { kind: "project", project_id: "p1" },
        sort: { field: "priority", direction: "desc" },
      }),
    );
    expect(listTaskTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({
          scope: { kind: "project", project_id: "p1" },
        }),
        group: { kind: "status" },
      }),
    );
  });

  it("uses the unified workspace query for workspace scope", async () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalled());

    expect(result.current.scopeKey).toBe("workspace:all");
    expect(result.current.filter).toEqual({});
    expect(result.current.loadMoreScope).toBeUndefined();
    expect(result.current.loadMoreFilter).toBeUndefined();
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "workspace",
    });
    expect(listTaskTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        group_key: "status:backlog",
        page: { limit: 50, cursor: null },
      }),
    );
  });

  it("does not subscribe List to the legacy task endpoint", async () => {
    const legacyListTasks = vi.fn(() => never<ListTasksResponse>());
    const tableRows = vi.fn(async (request: any) => ({
      query_fingerprint: "test",
      group_key: request.group_key,
      parent_id: null,
      total: 0,
      rows: [],
      branch_total: 0,
      next_cursor: null,
    }));
    const tableFacets = vi.fn(async () => ({
      query_fingerprint: "test",
      total: 0,
      facets: [{ kind: "status" as const, values: [] }],
    }));
    setApiInstance({
      listTasks: legacyListTasks,
      listTaskTableRows: tableRows,
      listTaskTableFacets: tableFacets,
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot,
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(tableRows).toHaveBeenCalled();
    expect(tableFacets).toHaveBeenCalled();
    expect(legacyListTasks).not.toHaveBeenCalled();
  });

  it("maps my assigned scope to the unified personal query contract", async () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "user-1" };
    expect(result.current.scopeKey).toBe("my:user-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "my",
      relation: "assigned",
    });
  });

  it("keeps actor scopes keyed by actor in the unified query shape", async () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: {
            type: "actor",
            actorType: "agent",
            actorId: "agent-1",
            relation: "assigned",
          },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "actor:agent:agent-1:assigned") },
    );

    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "agent-1" };
    expect(result.current.scopeKey).toBe("actor:agent:agent-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("actor:agent:agent-1:assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "assignee",
      actor: { type: "agent", id: "agent-1" },
    });
  });

  it.each([
    {
      name: "project",
      surfaceKey: "project:p1",
      scope: { type: "project" as const, projectId: "p1" },
      expected: { project_id: "p1", status: "todo" },
    },
    {
      name: "my assigned",
      surfaceKey: "my:user-1:assigned",
      scope: { type: "my" as const, relation: "assigned" as const, userId: "user-1" },
      expected: {
        assignee_type: "member",
        assignee_id: "user-1",
        status: "todo",
      },
    },
    {
      name: "actor assigned",
      surfaceKey: "actor:agent:agent-1:assigned",
      scope: {
        type: "actor" as const,
        actorType: "agent" as const,
        actorId: "agent-1",
        relation: "assigned" as const,
      },
      expected: {
        assignee_type: "agent",
        assignee_id: "agent-1",
        status: "todo",
      },
    },
  ])("merges $name create defaults into the create modal payload", ({ scope, surfaceKey, expected }) => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope,
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, surfaceKey) },
    );

    act(() => {
      result.current.openCreateTask({ status: "todo" });
    });

    expect(openModal).toHaveBeenCalledWith("create-task", expected);
  });

  it("clears surface selection when the view mode changes within the same scope", async () => {
    const store = getTaskSurfaceViewStore("my:user-1:assigned");
    store.getState().setViewMode("list");

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    act(() => {
      result.current.selection.select(["task-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["task-1"]));

    act(() => {
      store.getState().setViewMode("board");
    });

    // Synchronous on purpose: the reset happens during render (not in an
    // effect), so no committed frame pairs the new view with the old
    // selection.
    expect(result.current.viewMode).toBe("board");
    expect(result.current.selection.selectedIds).toEqual(new Set());
  });

  it("delegates drag movement as a server-owned relative intent", () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );
    const onSettled = vi.fn();

    act(() => {
      result.current.moveTask(
        "task-1",
        {
          status: "in_progress",
          position: 42,
          project_id: "p2",
          before_id: "task-0",
          after_id: "task-2",
        },
        onSettled,
      );
    });

    expect(updateTaskMutate).toHaveBeenCalledWith(
      {
        id: "task-1",
        status: "in_progress",
        position: 42,
        project_id: "p2",
        move_intent: {
          before_id: "task-0",
          after_id: "task-2",
        },
      },
      expect.objectContaining({
        onError: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );

    const options = updateTaskMutate.mock.calls[0]?.[1] as
      | { onSettled?: () => void }
      | undefined;
    options?.onSettled?.();
    expect(onSettled).toHaveBeenCalled();
  });

  it("exposes surface actions and surface-local selection", async () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    act(() => {
      result.current.selection.select(["task-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["task-1"]));

    await act(async () => {
      await result.current.actions.batchUpdate(["task-1"], { status: "done" });
      await result.current.actions.batchDelete(["task-2"]);
    });

    expect(batchUpdateMutateAsync).toHaveBeenCalledWith({
      ids: ["task-1"],
      updates: { status: "done" },
    });
    expect(batchDeleteMutateAsync).toHaveBeenCalledWith(["task-2"]);
  });

  it("never reports isEmpty in gantt mode — an empty scheduled subset cannot prove the window is empty", async () => {
    // The gantt query returns only tasks with a start/due date. A project
    // full of unscheduled tasks comes back [] here, and the surface used to
    // conclude "no tasks linked" and render the generic create-task empty
    // state over GanttView's accurate "no scheduled tasks" one.
    listTasks.mockResolvedValue({ tasks: [], total: 0 });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.viewMode).toBe("gantt");
    // Falls through to GanttView, which renders its own scheduled-empty copy.
    expect(result.current.isEmpty).toBe(false);
  });

  it("reports isRefreshing while a view change revalidates behind the previous snapshot", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    listTasks.mockResolvedValue({ tasks: [], total: 0 });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    // First load is loading, never refreshing — there is no previous
    // snapshot to show as a placeholder.
    expect(result.current.isLoading).toBe(true);
    expect(result.current.isRefreshing).toBe(false);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Sort change: the key changes, the previous order stays rendered as a
    // placeholder while the new order fetches — refreshing, NOT loading.
    const resolvers: ((r: ListTasksResponse) => void)[] = [];
    listTasks.mockImplementation(
      () => new Promise<ListTasksResponse>((res) => resolvers.push(res)),
    );
    act(() => store.getState().setSortBy("priority"));

    await waitFor(() => expect(result.current.isRefreshing).toBe(true));
    expect(result.current.isLoading).toBe(false);

    // The revalidation lands — the indicator clears.
    await act(async () => {
      for (const resolve of resolvers) resolve({ tasks: [], total: 0 });
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(false));
  });

  it("debounces table search into the canonical server query without fetching the legacy flat window", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    listTasks.mockResolvedValue({ tasks: [], total: 0 });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    listTasks.mockClear();

    act(() => result.current.setTableSearch("  Release train  "));

    expect(result.current.tableSearch).toBe("  Release train  ");
    expect(result.current.tableQuerySpec.search).toBeUndefined();
    await waitFor(() =>
      expect(result.current.tableQuerySpec.search).toBe("Release train"),
    );
    expect(listTasks).not.toHaveBeenCalled();
    expect(result.current.isEmpty).toBe(false);
  });

  it("loads only the Table facet whose filter submenu is active", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const listTaskTableFacets = vi.fn().mockResolvedValue({
      query_fingerprint: "sha256:facets",
      total: 0,
      facets: [{ kind: "status", values: [{ key: "todo", count: 2 }] }],
    });
    setApiInstance({
      listTasks,
      listTaskTableFacets,
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    expect(listTaskTableFacets).not.toHaveBeenCalled();
    act(() => result.current.setActiveTableFacet({ kind: "status" }));
    await waitFor(() => expect(listTaskTableFacets).toHaveBeenCalledTimes(1));
    expect(listTaskTableFacets).toHaveBeenCalledWith(
      expect.objectContaining({
        facets: [{ kind: "status" }],
        include_total: false,
      }),
    );
    await waitFor(() =>
      expect(result.current.tableFacetCounts?.facets[0]?.kind).toBe("status"),
    );

    act(() => result.current.setActiveTableFacet(null));
    expect(result.current.tableFacetCounts).toBeUndefined();
  });

  it.each([
    {
      name: "Assignee Board",
      configure: (store: ReturnType<typeof getTaskSurfaceViewStore>) => {
        store.getState().setViewMode("board");
        store.getState().setGrouping("assignee");
      },
      properties: [],
    },
    {
      name: "Property Board",
      configure: (store: ReturnType<typeof getTaskSurfaceViewStore>) => {
        store.getState().setViewMode("board");
        store.getState().setGrouping("property:severity");
      },
      properties: [
        {
          id: "severity",
          workspace_id: "ws-1",
          name: "Severity",
          type: "select",
          config: { options: [] },
          position: 0,
          archived: false,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    },
    {
      name: "Swimlane",
      configure: (store: ReturnType<typeof getTaskSurfaceViewStore>) => {
        store.getState().setViewMode("swimlane");
      },
      properties: [],
    },
  ])(
    "loads exact filter facets on demand for the server-paged $name",
    async ({ configure, properties }) => {
      const store = getTaskSurfaceViewStore("project:p1");
      configure(store);
      const listTaskTableFacets = vi.fn().mockResolvedValue({
        query_fingerprint: "sha256:group-facets",
        total: 0,
        facets: [{ kind: "priority", values: [{ key: "high", count: 37 }] }],
      });
      const tableMethods = statusTableMethodsFromLegacy(listTasks);
      setApiInstance({
        listTasks,
        ...tableMethods,
        listTaskTableFacets,
        listGroupedTasks: vi.fn(() => never()),
        listProjects: vi.fn(() => never()),
        listProperties: vi.fn(() =>
          Promise.resolve({ properties, total: properties.length }),
        ),
        getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
        getChildTaskProgress: vi.fn(() => Promise.resolve([])),
      } as unknown as ApiClient);

      const { result } = renderHook(
        () =>
          useTaskSurfaceController({
            scope: { type: "project", projectId: "p1" },
            modes: ["board", "list", "swimlane"],
          }),
        { wrapper: makeWrapper(qc, "project:p1") },
      );

      expect(result.current.facetCountsExact).toBe(false);
      expect(listTaskTableFacets).not.toHaveBeenCalled();

      act(() => result.current.setActiveTableFacet({ kind: "priority" }));

      await waitFor(() => expect(listTaskTableFacets).toHaveBeenCalledTimes(1));
      expect(listTaskTableFacets).toHaveBeenCalledWith(
        expect.objectContaining({
          facets: [{ kind: "priority" }],
          include_total: false,
        }),
      );
      await waitFor(() =>
        expect(result.current.tableFacetCounts?.facets).toEqual([
          { kind: "priority", values: [{ key: "high", count: 37 }] },
        ]),
      );

      act(() => result.current.setActiveTableFacet(null));
      expect(result.current.tableFacetCounts).toBeUndefined();
    },
  );

  it("fails Table export closed when schema fallback would truncate the CSV", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const listTaskTableRows = vi.fn(() =>
      Promise.resolve({
        query_fingerprint: "",
        group_key: null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      }),
    );
    setApiInstance({
      listTasks,
      listTaskTableRows,
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await expect(result.current.exportTableTasks()).rejects.toBeInstanceOf(
      TaskTableExportIntegrityError,
    );
  });

  it("exports every stable cursor page exactly once", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const first = makeTask({ id: "task-1", status: "todo" });
    const second = makeTask({ id: "task-2", status: "done" });
    const listTaskTableRows = vi
      .fn()
      .mockResolvedValueOnce({
        query_fingerprint: "sha256:export",
        group_key: null,
        parent_id: null,
        total: 2,
        rows: [{ task: first, direct_child_count: 0 }],
        branch_total: 2,
        next_cursor: "cursor-2",
      })
      .mockResolvedValueOnce({
        query_fingerprint: "sha256:export",
        group_key: null,
        parent_id: null,
        total: 0,
        rows: [{ task: second, direct_child_count: 0 }],
        branch_total: 1,
        next_cursor: null,
      });
    setApiInstance({
      listTasks,
      listTaskTableRows,
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await expect(result.current.exportTableTasks()).resolves.toEqual([
      first,
      second,
    ]);
    expect(listTaskTableRows).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ page: { limit: 100, cursor: "cursor-2" } }),
    );
  });

  it("sends workspace running-task task ids through the Table filter", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    store.getState().toggleAgentRunningFilter();
    listTasks.mockResolvedValue({ tasks: [], total: 0 });
    const getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([
        {
          id: "agent-1",
          name: "Agent 1",
          avatar_url: null,
          running_run_count: 1,
          task_ids: ["task-running"],
        },
        {
          id: "agent-2",
          name: "Agent 2",
          avatar_url: null,
          running_run_count: 2,
          task_ids: ["task-running-2"],
        },
      ] satisfies WorkspaceWorkingAgent[]),
    );
    setApiInstance({
      listTasks,
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot: vi.fn(() =>
        Promise.resolve([
          { id: "task-1", task_id: "task-running", status: "running" },
        ] as unknown as AgentRun[]),
      ),
      getWorkspaceWorkingAgents,
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual([
        "task-running",
        "task-running-2",
      ]),
    );
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("task", undefined, undefined);
    expect(listTasks).not.toHaveBeenCalled();
  });

  it("uses the active My Tasks relation for the Table working-agent filter", async () => {
    const store = getTaskSurfaceViewStore("my:user-1:assigned");
    store.getState().setViewMode("table");
    store.getState().toggleAgentRunningFilter();
    const getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([] satisfies WorkspaceWorkingAgent[]),
    );
    setApiInstance({
      listTasks,
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getWorkspaceWorkingAgents,
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    await waitFor(() =>
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith(
        "task",
        "assigned",
        undefined,
      ),
    );
    expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual([]);
  });

  it.each(["board", "list", "swimlane"] as const)(
    "uses running tasks for the %s server query without reading the task snapshot",
    async (viewMode) => {
      const store = getTaskSurfaceViewStore("project:p1");
      store.getState().setViewMode(viewMode);
      store.getState().toggleAgentRunningFilter();
      getWorkspaceWorkingAgents.mockResolvedValue([
        makeWorkingAgent("agent-from-working-api", ["task-from-working-api"]),
      ]);
      // Deliberately contradictory legacy data. It must neither be fetched nor
      // influence membership after the quick filter moved to working-agents.
      getAgentRunSnapshot.mockResolvedValue([
        makeRunningTask("legacy-task", "legacy-agent", "legacy-task"),
      ]);

      const { result } = renderHook(
        () =>
          useTaskSurfaceController({
            scope: { type: "project", projectId: "p1" },
            modes: ["board", "list", "swimlane"],
          }),
        { wrapper: makeWrapper(qc, "project:p1") },
      );

      await waitFor(() =>
        expect(
          result.current.tableQuerySpec.filters.working_task_ids,
        ).toEqual(["task-from-working-api"]),
      );
      expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
      expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("task", undefined, undefined);
      expect(getAgentRunSnapshot).not.toHaveBeenCalled();
    },
  );

  it("combines regular assignees with the independent running-task predicate", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    store.getState().toggleAssigneeFilter({ type: "agent", id: "agent-1" });
    store.getState().toggleAssigneeFilter({ type: "agent", id: "agent-2" });
    store.getState().toggleAssigneeFilter({ type: "member", id: "member-1" });
    store.getState().toggleNoAssignee();
    store.getState().toggleAgentRunningFilter();
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-2", ["member-assigned-running-task"]),
      makeWorkingAgent("agent-3", ["unassigned-running-task"]),
    ]);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => {
      expect(result.current.tableQuerySpec.filters.assignees).toEqual([
        { type: "agent", id: "agent-1" },
        { type: "agent", id: "agent-2" },
        { type: "member", id: "member-1" },
      ]);
      expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual([
        "member-assigned-running-task",
        "unassigned-running-task",
      ]);
    });
    expect(result.current.tableQuerySpec.filters.include_no_assignee).toBe(true);
  });

  it("does not subscribe Table to the legacy offset window", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    // page 1 claims a small window (under the ceiling); by page 2 the real
    // window has grown far beyond it. The ceiling check must see the fresh
    // total — pagination itself already advances on it.
    listTasks.mockImplementation((params?: ListTasksParams) =>
      Promise.resolve(
        (params?.offset ?? 0) === 0
          ? {
              tasks: [
                makeTask({ id: "i-1", status: "todo" }),
                makeTask({ id: "i-2", status: "todo" }),
              ],
              total: 900,
            }
          : { tasks: [makeTask({ id: "i-3", status: "todo" })], total: 50_000 },
      ),
    );

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    expect(result.current.isLoading).toBe(false);
    expect(result.current.isEmpty).toBe(false);
    expect(listTasks).not.toHaveBeenCalled();
  });

  it("leaves Table empty/error ownership to the server-backed renderer", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    listTasks.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    // The legacy list endpoint is not part of Table rendering anymore.
    expect(listTasks).not.toHaveBeenCalled();
    expect(result.current.isEmpty).toBe(false);
  });

  it("clears surface selection when the membership window changes (filters, search)", async () => {
    const store = getTaskSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    listTasks.mockResolvedValue({ tasks: [], total: 0 });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    act(() => {
      result.current.selection.select(["task-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["task-1"]));

    // Batch actions mutate raw selected ids while export/common-field
    // consumers intersect with visible rows — a selection surviving a
    // membership change would let the same "1 selected" mean different sets.
    // Asserted synchronously: the reset is render-phase, so not even one
    // frame commits the new membership with the old selection.
    act(() => {
      store.getState().toggleStatusFilter("todo");
    });

    expect(result.current.selection.selectedIds).toEqual(new Set());
  });

  it("still reports isEmpty for the full-window modes when the list is empty", async () => {
    listTasks.mockResolvedValue({ tasks: [], total: 0 });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.isEmpty).toBe(true);
  });

  // --- cancelled as a default status (SILI-4290) ------------------------
  // Cancelled is a first-class default lifecycle status: fetched into the
  // cache, surfaced by default, narrowed (not unlocked) by the status filter,
  // and hideable like any other status.

  function mockListByStatus(byStatus: Partial<Record<TaskStatus, Task[]>>) {
    listTasks.mockImplementation((params?: ListTasksParams) => {
      const status = params?.status as TaskStatus | undefined;
      const tasks = (status && byStatus[status]) ?? [];
      return Promise.resolve({ tasks, total: tasks.length });
    });
  }

  it("fetches and surfaces the cancelled bucket as a default status", async () => {
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(listTasks).toHaveBeenCalled());

    // The fetch layer requests the cancelled status page like any other.
    expect(listTasks).toHaveBeenCalledWith(
      expect.objectContaining({ status: "cancelled", limit: 50, offset: 0 }),
    );
    // …and with no status filter it is a visible column, ordered last.
    expect(result.current.visibleStatuses).toContain("cancelled");
    expect(result.current.visibleStatuses.at(-1)).toBe("cancelled");
  });

  it("includes cancelled tasks in the default surface and visible statuses", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
      cancelled: [makeTask({ id: "cancelled-1", status: "cancelled" })],
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.visibleStatuses).toContain("cancelled");
    const surfaceIds = result.current.surfaceTasks.map((i) => i.id);
    expect(surfaceIds).toContain("todo-1");
    expect(surfaceIds).toContain("cancelled-1");
    expect(result.current.tasks.map((i) => i.id)).toContain("cancelled-1");
  });

  it("narrows the visible set to the selected statuses, dropping cancelled when it is not selected", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
      cancelled: [makeTask({ id: "cancelled-1", status: "cancelled" })],
    });

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => store.getState().toggleStatusFilter("todo"));

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // The filter narrows the rendered columns and their contents — cancelled
    // is a normal status the filter can exclude, not an unlockable bucket.
    expect(result.current.visibleStatuses).toEqual(["todo"]);
    expect(result.current.tasks.map((i) => i.id)).toEqual(["todo-1"]);
    // cancelled participates in show/hide like the rest — hidden here because
    // the active filter excludes it.
    expect(result.current.hiddenStatuses).toContain("cancelled");
  });

  it("treats a cancelled-only filter like any other narrowing status filter", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
      cancelled: [makeTask({ id: "cancelled-1", status: "cancelled" })],
    });

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => store.getState().toggleStatusFilter("cancelled"));

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Cancelled becomes the sole visible column and the surface narrows to it.
    expect(result.current.visibleStatuses).toEqual(["cancelled"]);
    expect(result.current.tasks.map((i) => i.id)).toEqual(["cancelled-1"]);
    expect(result.current.surfaceTasks.map((i) => i.id)).toContain(
      "cancelled-1",
    );
  });

  // --- working-chip scope (SILI-4884) ------------------------------------
  // The header chip promises "N tasks in progress" where N is the number of
  // rows clicking it leaves. The working-agents endpoint identifies both the
  // running agents and the tasks referenced by those agents' running tasks.

  it("sends working task ids to the server without reconstructing a local scope", async () => {
    mockListByStatus({
      todo: [
        makeTask({ id: "todo-1", status: "todo" }),
        makeTask({ id: "todo-2", status: "todo" }),
      ],
      in_progress: [makeTask({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
    ]);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => store.getState().toggleAgentRunningFilter());

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual([
      "todo-1",
      "prog-1",
    ]);
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(getAgentRunSnapshot).not.toHaveBeenCalled();
    // Membership is cursor-paged and server-owned. A partial page cannot
    // produce the exact activity-chip scope, so it stays explicitly unknown.
    expect(result.current.workingScopeTasks).toBeUndefined();
  });

  it("keeps the activity-chip scope unknown before the server filter is enabled", async () => {
    mockListByStatus({
      todo: [
        makeTask({ id: "todo-1", status: "todo" }),
        makeTask({ id: "todo-2", status: "todo" }),
      ],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
    ]);

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Filter off: the list still shows both loaded rows, while the exact
    // post-click membership remains unknown until the backend predicate runs.
    expect(result.current.tasks).toHaveLength(2);
    expect(result.current.workingScopeTasks).toBeUndefined();
  });

  it("combines the status and working predicates in the canonical server query", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
      in_progress: [makeTask({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
    ]);

    // ...but the user is only looking at `todo`.
    const store = getTaskSurfaceViewStore("project:p1");
    act(() => {
      store.getState().toggleStatusFilter("todo");
      store.getState().toggleAgentRunningFilter();
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.tableQuerySpec.filters.statuses).toEqual(["todo"]);
    expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual([
      "todo-1",
      "prog-1",
    ]);
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(result.current.workingScopeTasks).toBeUndefined();
  });

  it("sends the sub-task display rule to the same server query", async () => {
    mockListByStatus({
      todo: [
        makeTask({ id: "parent-1", status: "todo" }),
        makeTask({ id: "child-1", status: "todo", parent_task_id: "parent-1" }),
      ],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["parent-1"]),
    ]);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => store.getState().toggleShowSubTasks());

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.tableQuerySpec.filters.include_sub_tasks).toBe(false);
    expect(result.current.workingScopeTasks).toBeUndefined();
  });

  it("requests task working agents so chat/autopilot work stays out of scope", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
    });
    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.workingScopeTasks).toEqual([]);
    expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("task", undefined, undefined);
    expect(getAgentRunSnapshot).not.toHaveBeenCalled();
  });

  it("keeps swimlane chrome bounded while descriptors retain hidden-status counts", async () => {
    mockListByStatus({
      todo: [makeTask({ id: "todo-1", status: "todo" })],
      in_progress: [makeTask({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
    ]);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("swimlane");
      store.getState().toggleStatusFilter("todo");
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    // Like Table/List, the migrated Swimlane owns cursor branches and does
    // not materialize another complete working-only window for the chip.
    expect(result.current.workingScopeTasks).toBeUndefined();
    // Controller-only tests do not mount lane cells, so no row branch should
    // activate merely because its descriptor exists.
    expect(result.current.tasks).toEqual([]);
    expect(result.current.swimlaneTasks).toEqual([]);
    expect(
      result.current.groupBranches?.descriptors
        .flatMap((lane) => lane.secondary_groups ?? [])
        .map((cell) => cell.value.kind === "status" ? cell.value.status : "")
        .sort(),
    ).toEqual(["in_progress", "todo"]);
  });

  // --- gantt canvas scope ------------------------------------------------
  // The gantt canvas draws fewer rows than the shared filters leave: a row
  // needs a date, and done/cancelled hide unless `ganttShowCompleted` is on.
  // Those rules live in the surface (`ganttCanvasRows`) so the chip narrows
  // the same set the canvas draws.

  function mockGanttTasks(tasks: Task[]) {
    listTasks.mockImplementation((params?: ListTasksParams) => {
      if (params?.scheduled === true) {
        return Promise.resolve({ tasks, total: tasks.length });
      }
      return Promise.resolve({ tasks: [], total: 0 });
    });
  }

  const ganttFixture = [
    makeTask({
      id: "gantt-open",
      status: "in_progress",
      assignee_type: "agent",
      assignee_id: "agent-1",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    makeTask({
      id: "gantt-done",
      status: "done",
      assignee_type: "agent",
      assignee_id: "agent-2",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    // Scheduled server-side but momentarily dateless (e.g. a WS patch that
    // just cleared both dates) — the canvas cannot place it.
    makeTask({
      id: "gantt-undated",
      status: "in_progress",
      assignee_type: "agent",
      assignee_id: "agent-3",
    }),
  ];

  it("filters Gantt by running-task task ids rather than task assignees", async () => {
    mockGanttTasks(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue([
      // The editing agent deliberately differs from the task assignee.
      makeWorkingAgent("agent-editor", ["gantt-open"]),
    ]);
    // Contradictory legacy membership must not affect the canvas.
    getAgentRunSnapshot.mockResolvedValue([
      makeRunningTask("t-2", "agent-2", "gantt-done"),
      makeRunningTask("t-3", "agent-3", "gantt-undated"),
    ]);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleAgentRunningFilter();
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.filteredGanttTasks.length).toBe(1),
    );

    // ganttShowCompleted defaults to false, so the done row and the undated
    // row are not drawn — the chip must not count them either.
    expect(result.current.workingScopeTasks?.map((i) => i.id)).toEqual([
      "gantt-open",
    ]);
    expect(result.current.workingScopeTasks?.map((i) => i.id)).toEqual(
      result.current.filteredGanttTasks.map((i) => i.id),
    );
    expect(getAgentRunSnapshot).not.toHaveBeenCalled();
  });

  it.each([
    {
      name: "the working-agent API returns no agents",
      workingAgents: [] as WorkspaceWorkingAgent[],
      selectedAssigneeId: null,
    },
    {
      name: "the selected assignee excludes all running-task tasks",
      workingAgents: [makeWorkingAgent("agent-1", ["gantt-open"])],
      selectedAssigneeId: "agent-2",
    },
  ])("keeps Gantt empty when $name", async ({
    workingAgents,
    selectedAssigneeId,
  }) => {
    mockGanttTasks(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue(workingAgents);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      if (selectedAssigneeId) {
        store.getState().toggleAssigneeFilter({
          type: "agent",
          id: selectedAssigneeId,
        });
      }
      store.getState().toggleAgentRunningFilter();
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.ganttTasks).toHaveLength(3));
    await waitFor(() =>
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith(
        "task",
        undefined,
        undefined,
      ),
    );

    expect(result.current.tableQuerySpec.filters.working_task_ids).toEqual(
      workingAgents.flatMap((agent) => agent.task_ids),
    );
    expect(result.current.tableQuerySpec.filters.assignees).toEqual(
      selectedAssigneeId
        ? [{ type: "agent", id: selectedAssigneeId }]
        : undefined,
    );
    expect(result.current.filteredGanttTasks).toEqual([]);
    expect(result.current.workingScopeTasks).toEqual([]);
  });

  it("widens the gantt working scope when show-completed is turned on", async () => {
    mockGanttTasks(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-editor", ["gantt-open", "gantt-done"]),
    ]);

    const store = getTaskSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleAgentRunningFilter();
      store.getState().toggleGanttShowCompleted();
    });

    const { result } = renderHook(
      () =>
        useTaskSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.filteredGanttTasks.length).toBe(2),
    );

    // The done row is drawn now, so it counts. The undated one still cannot
    // be placed, so it still does not.
    expect(result.current.workingScopeTasks?.map((i) => i.id).sort()).toEqual([
      "gantt-done",
      "gantt-open",
    ]);
    expect(result.current.workingScopeTasks?.map((i) => i.id)).toEqual(
      result.current.filteredGanttTasks.map((i) => i.id),
    );
    expect(getAgentRunSnapshot).not.toHaveBeenCalled();
  });
});
