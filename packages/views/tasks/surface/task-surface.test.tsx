/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@silieco/core/api";
import type { ApiClient } from "@silieco/core/api/client";
import { pruneTaskSurfaceViewStates } from "@silieco/core/tasks/stores/surface-view-store";
import type {
  AgentRun,
  Task,
  TaskTableRowsRequest,
  ListTasksParams,
  ListTasksResponse,
} from "@silieco/core/types";
import { TaskSurface } from "./task-surface";
import { statusTableMethodsFromLegacy } from "./status-table-test-api";

// Mutable so tests can simulate a workspace switch — the workspace layout
// does not remount its children on switch, so the surface must handle the
// wsId change itself.
const mockWsId = vi.hoisted(() => ({ current: "ws-1" }));
const mockTranslate = vi.hoisted(() => vi.fn(() => "translated"));
vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => mockWsId.current,
}));

// The list/board virtualize their rows via react-virtuoso; jsdom has no layout
// so the real Virtuoso renders nothing (and throws on its resize plumbing).
// Render items inline so these surface-level loading-semantics assertions still
// see the tasks the virtualized list would show.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent, components }: any) => (
    <div data-testid="virtuoso-mock">
      {(data ?? []).map((item: any, i: number) => (
        <div key={i}>{itemContent(i, item)}</div>
      ))}
      {components?.Footer ? <components.Footer /> : null}
    </div>
  ),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count, estimateSize }: any) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        start: index * estimateSize(),
        end: (index + 1) * estimateSize(),
      })),
    getTotalSize: () => count * estimateSize(),
  }),
}));

const mockAuthUser = { id: "user-1", email: "test@test.com", name: "Test User" };
vi.mock("@silieco/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

vi.mock("../../i18n", () => ({
  // TableView also reads `i18n.language` for its date formatting.
  useT: () => ({ t: mockTranslate, i18n: { language: "en" } }),
  useTimeAgo: () => () => "now",
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/" }),
}));

vi.mock("@silieco/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@silieco/core/paths")>(
    "@silieco/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test WS", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

function makeTask(id: string, title: string, projectId: string): Task {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `SILI-${id}`,
    title,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: projectId,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function never<T>() {
  return new Promise<T>(() => {});
}

function projectSurface(projectId: string) {
  return (
    <TaskSurface
      scope={{ type: "project", projectId }}
      modes={["list"]}
      renderHeader={() => null}
      renderLoading={() => <div data-testid="surface-loading" />}
      batchToolbar="never"
    />
  );
}

describe("TaskSurface — scope switch loading semantics", () => {
  let qc: QueryClient;

  beforeEach(() => {
    mockWsId.current = "ws-1";
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    // p1 answers immediately with one task; p2 stays in flight forever so
    // the test can observe the in-between state after switching.
    const listTasks = vi.fn((params?: ListTasksParams) => {
      if (params?.project_id === "p2") return never<ListTasksResponse>();
      const tasks =
        params?.status === "todo" ? [makeTask("i1", "P1 task", "p1")] : [];
      return Promise.resolve({ tasks, total: tasks.length });
    });
    setApiInstance({
      listTasks,
      ...statusTableMethodsFromLegacy(listTasks),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot: vi.fn(() => never<AgentRun[]>()),
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);
    pruneTaskSurfaceViewStates([]);
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneTaskSurfaceViewStates([]);
    vi.restoreAllMocks();
  });

  it("shows loading — not the previous project's tasks — while the next project is fetching", async () => {
    // Regression: the list queries use `placeholderData: keepPreviousData` to
    // keep sort/filter changes flicker-free WITHIN one surface. Without a
    // scope-keyed remount, that placeholder leaks ACROSS surfaces: switching
    // pinned projects kept rendering project A's cards (isLoading=false, so
    // no skeleton either) until project B's response landed — the "click does
    // nothing, then it snaps" bug.
    const { rerender } = render(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );

    await screen.findByText("P1 task");

    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p2")}</QueryClientProvider>,
    );

    // The switch must be honest: p2 has no data yet, so the surface is
    // loading — p1's cards must not impersonate p2.
    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();
    expect(screen.queryByText("P1 task")).not.toBeInTheDocument();
  });

  it("shows a cached project instantly on switch-back (no loading flash)", async () => {
    const { rerender } = render(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );
    await screen.findByText("P1 task");

    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p2")}</QueryClientProvider>,
    );
    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();

    // Back to p1: its cache is warm, so the list renders immediately from
    // cache — remounting must not degrade the instant-switch path.
    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );
    await waitFor(() =>
      expect(screen.getByText("P1 task")).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("surface-loading")).not.toBeInTheDocument();
  });

  it("shows loading on a workspace switch even though the scope key is identical", async () => {
    // The workspace layout does NOT remount children on switch, and two
    // workspaces share the same scope key (e.g. "workspace:all") — so the
    // remount key must include wsId, or workspace A's tasks impersonate
    // workspace B's while B is still fetching.
    //
    // A fresh element per render — reusing one element reference would let
    // React bail out of re-rendering the subtree entirely, and the wsId
    // change would never propagate.
    const workspaceSurface = () => (
      <TaskSurface
        scope={{ type: "workspace" }}
        modes={["list"]}
        renderHeader={() => null}
        renderLoading={() => <div data-testid="surface-loading" />}
        batchToolbar="never"
      />
    );

    const listTasks = vi.fn((params?: ListTasksParams) => {
      const tasks =
        params?.status === "todo" ? [makeTask("i1", "WS1 task", "p1")] : [];
      return Promise.resolve({ tasks, total: tasks.length });
    });
    setApiInstance({
      listTasks,
      ...statusTableMethodsFromLegacy(listTasks),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot: vi.fn(() => never<AgentRun[]>()),
      getChildTaskProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { rerender } = render(
      <QueryClientProvider client={qc}>{workspaceSurface()}</QueryClientProvider>,
    );
    await screen.findByText("WS1 task");

    // Switch workspace: same scope, new wsId, and the new workspace's
    // fetches hang so the in-between state is observable.
    listTasks.mockImplementation(() => never<ListTasksResponse>());
    mockWsId.current = "ws-2";
    rerender(
      <QueryClientProvider client={qc}>{workspaceSurface()}</QueryClientProvider>,
    );

    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();
    expect(screen.queryByText("WS1 task")).not.toBeInTheDocument();
  });
});

describe("TaskSurface — table pagination ownership", () => {
  let qc: QueryClient;
  let listTasks: ReturnType<
    typeof vi.fn<(params?: ListTasksParams) => Promise<ListTasksResponse>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listTasks = vi.fn(() => never<ListTasksResponse>());
    mockTranslate.mockClear();
    pruneTaskSurfaceViewStates([]);
    mockWsId.current = "ws-1";
    // jsdom has no IntersectionObserver; the table footer sentinel constructs
    // one on mount. A stub that never fires keeps the sentinel inert, so the
    // structure loop is the only automatic pagination driver under test.
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneTaskSurfaceViewStates([]);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("does not materialize the legacy offset window and starts one cursor root branch", async () => {
    const { getTaskSurfaceViewStore } = await import(
      "@silieco/core/tasks/stores/surface-view-store"
    );
    const store = getTaskSurfaceViewStore("project:pt");
    store.getState().setViewMode("table");
    if (!store.getState().agentRunningFilter) {
      store.getState().toggleAgentRunningFilter();
    }

    const runningTasks = Array.from({ length: 250 }, (_, index) => ({
      ...makeTask(`run-${index}`, `Running ${index}`, "pt"),
      status: "in_progress" as const,
    }));
    const listTaskTableRows = vi.fn(() => never());
    setApiInstance({
      listTasks,
      listTaskTableRows,
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentRunSnapshot: vi.fn(() =>
        Promise.resolve(
          runningTasks.map((task, index) => ({
            id: `task-${index}`,
            agent_id: `agent-${index}`,
            task_id: task.id,
            status: "running",
          })) as unknown as AgentRun[],
        ),
      ),
      getWorkspaceWorkingAgents: vi.fn(() =>
        Promise.resolve(
          runningTasks.map((task, index) => ({
            id: `agent-${index}`,
            name: `Agent ${index}`,
            avatar_url: null,
            running_run_count: 1,
            task_ids: [task.id],
          })),
        ),
      ),
      getChildTaskProgress: vi.fn(() => never()),
      listProperties: vi.fn(() => never()),
      listMembers: vi.fn(() => never()),
      listAgents: vi.fn(() => never()),
      listSquads: vi.fn(() => never()),
    } as unknown as ApiClient);

    render(
      <QueryClientProvider client={qc}>
        <TaskSurface
          scope={{ type: "project", projectId: "pt" }}
          modes={["table"]}
          renderHeader={() => null}
          renderLoading={() => <div data-testid="surface-loading" />}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    // The first render has not received the independent working-agents query
    // yet and therefore requests the explicit match-none form. Once that
    // query resolves, the Table owns a new query key containing the agent id
    // list and starts the real branch.
    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalledTimes(2));
    expect(listTaskTableRows).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group: { kind: "none" },
        group_key: null,
        parent_id: null,
        query: expect.objectContaining({
          filters: expect.objectContaining({
            working_task_ids: runningTasks.map((task) => task.id),
          }),
        }),
      }),
    );
    expect(listTasks).not.toHaveBeenCalled();
  });

  it("keeps loaded rows when a continuation page reports zero", async () => {
    const { getTaskSurfaceViewStore } = await import(
      "@silieco/core/tasks/stores/surface-view-store"
    );
    const store = getTaskSurfaceViewStore("project:pt-pages");
    store.getState().setViewMode("table");
    const first = makeTask("page-1", "First cursor row", "pt-pages");
    const second = makeTask("page-2", "Second cursor row", "pt-pages");
    const listTaskTableRows = vi.fn((request: TaskTableRowsRequest) =>
      Promise.resolve(
        request.page?.cursor == null
          ? {
              query_fingerprint: "sha256:pages",
              group_key: null,
              parent_id: null,
              total: 2,
              rows: [{ task: first, direct_child_count: 0 }],
              branch_total: 1,
              next_cursor: "cursor-2",
            }
          : {
              query_fingerprint: "sha256:pages",
              group_key: null,
              parent_id: null,
              total: 0,
              rows: [{ task: second, direct_child_count: 0 }],
              branch_total: 1,
              next_cursor: null,
            },
      ),
    );
    setApiInstance({
      listTasks,
      listTaskTableRows,
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    // Continuation is driven by the shared footer's sentinel, the same one
    // Board / List / Swimlane use — there is no manual button to press, so the
    // observer has to actually report the footer as visible.
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        private readonly callback: IntersectionObserverCallback;
        constructor(callback: IntersectionObserverCallback) {
          this.callback = callback;
        }
        observe(target: Element) {
          this.callback(
            [{ isIntersecting: true, target } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          );
        }
        unobserve() {}
        disconnect() {}
        takeRecords() {
          return [];
        }
        root = null;
        rootMargin = "0px";
        thresholds = [0];
      },
    );

    render(
      <QueryClientProvider client={qc}>
        <TaskSurface
          scope={{ type: "project", projectId: "pt-pages" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("First cursor row");
    await screen.findByText("Second cursor row");
    expect(listTaskTableRows).toHaveBeenCalledWith(
      expect.objectContaining({ page: { limit: 50, cursor: "cursor-2" } }),
    );
    expect(screen.getByText("First cursor row")).toBeInTheDocument();
  });

  it("feeds loaded Table rows to the shared batch toolbar", async () => {
    const { getTaskSurfaceViewStore } = await import(
      "@silieco/core/tasks/stores/surface-view-store"
    );
    const store = getTaskSurfaceViewStore("project:pt-batch");
    store.getState().setViewMode("table");
    const task = makeTask("table-selected", "Loaded Table task", "pt-batch");

    setApiInstance({
      listTasks,
      listTaskTableRows: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:table-batch",
          group_key: null,
          parent_id: null,
          total: 1,
          rows: [{ task, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: null,
        }),
      ),
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { container } = render(
      <QueryClientProvider client={qc}>
        <TaskSurface
          scope={{ type: "project", projectId: "pt-batch" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="always"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Loaded Table task");
    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[1]!);

    await waitFor(() => {
      expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
    });
  });

  it("keeps the previous Table rows painted while a new sort is loading", async () => {
    const { getTaskSurfaceViewStore } = await import(
      "@silieco/core/tasks/stores/surface-view-store"
    );
    const store = getTaskSurfaceViewStore("project:pt-sort-transition");
    store.getState().setViewMode("table");
    const task = makeTask(
      "table-sort-placeholder",
      "Table row kept during sort",
      "pt-sort-transition",
    );
    const listTaskTableRows = vi.fn((request: TaskTableRowsRequest) =>
      request.query.sort.field === "position"
        ? Promise.resolve({
            query_fingerprint: "sha256:initial-sort",
            group_key: null,
            parent_id: null,
            total: 1,
            rows: [{ task, direct_child_count: 0 }],
            branch_total: 1,
            next_cursor: null,
          })
        : never(),
    );
    setApiInstance({
      listTasks,
      listTaskTableRows,
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    render(
      <QueryClientProvider client={qc}>
        <TaskSurface
          scope={{ type: "project", projectId: "pt-sort-transition" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Table row kept during sort");
    act(() => store.getState().setSortBy("title"));
    await waitFor(() => expect(listTaskTableRows).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Table row kept during sort")).toBeInTheDocument();
  });

  it("keeps selected Table rows in the batch universe after their group collapses", async () => {
    const { getTaskSurfaceViewStore } = await import(
      "@silieco/core/tasks/stores/surface-view-store"
    );
    const store = getTaskSurfaceViewStore("project:pt-collapsed-batch");
    store.getState().setViewMode("table");
    store.getState().setTableGrouping("status");
    const task = makeTask(
      "table-collapsed-selected",
      "Selected task in collapsed group",
      "pt-collapsed-batch",
    );

    vi.stubGlobal(
      "IntersectionObserver",
      class {
        private readonly callback: IntersectionObserverCallback;
        constructor(callback: IntersectionObserverCallback) {
          this.callback = callback;
        }
        observe(target: Element) {
          this.callback(
            [{ isIntersecting: true, target } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          );
        }
        unobserve() {}
        disconnect() {}
        takeRecords() {
          return [];
        }
        root = null;
        rootMargin = "0px";
        thresholds = [0];
      },
    );

    setApiInstance({
      listTasks,
      listTaskTableGroups: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:collapsed-groups",
          total: 1,
          groups: [
            {
              key: "status:todo",
              value: { kind: "status", status: "todo" },
              count: 1,
            },
          ],
          next_cursor: null,
        }),
      ),
      listTaskTableRows: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:collapsed-rows",
          group_key: "status:todo",
          parent_id: null,
          total: 1,
          rows: [{ task, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: null,
        }),
      ),
      listTaskTableFacets: vi.fn(() => never()),
      listGroupedTasks: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentRunSnapshot: vi.fn(() => Promise.resolve([])),
      getChildTaskProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { container } = render(
      <QueryClientProvider client={qc}>
        <TaskSurface
          scope={{ type: "project", projectId: "pt-collapsed-batch" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="always"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Selected task in collapsed group");
    fireEvent.click(screen.getAllByRole("checkbox")[1]!);
    await waitFor(() => {
      expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
    });

    fireEvent.click(screen.getByRole("button", { name: "translated1" }));
    await waitFor(() => {
      expect(screen.queryByText("Selected task in collapsed group")).toBeNull();
    });
    expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
  });
});
