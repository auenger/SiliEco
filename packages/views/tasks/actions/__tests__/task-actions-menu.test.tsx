import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Task } from "@silieco/core/types";
import { I18nProvider } from "@silieco/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enTasks from "../../../locales/en/tasks.json";

const TEST_RESOURCES = { en: { common: enCommon, tasks: enTasks } };

// ---------------------------------------------------------------------------
// Mocks — same pattern as the task-detail test suite.
// ---------------------------------------------------------------------------

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockOpenModal = vi.fn();
vi.mock("@silieco/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: any) => {
      const state = { open: mockOpenModal };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

const mockAuthState = { user: { id: "user-1" }, isAuthenticated: true };
vi.mock("@silieco/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => (selector ? selector(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  ),
  registerAuthStore: vi.fn(),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "members"],
    queryFn: () =>
      Promise.resolve([
        { user_id: "user-1", name: "Test User", email: "t@t.com", role: "admin" },
      ]),
  }),
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
  squadListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "squads"],
    queryFn: () => Promise.resolve([]),
  }),
  assigneeFrequencyOptions: () => ({
    queryKey: ["workspaces", "ws-1", "assignee-frequency"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@silieco/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_t: string, _id: string) => "" }),
}));

vi.mock("@silieco/core/pins", () => ({
  pinListOptions: () => ({
    queryKey: ["pins", "ws-1", "user-1"],
    queryFn: () => Promise.resolve([]),
  }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));

vi.mock("@silieco/core/tasks/mutations", () => ({
  useUpdateTask: () => ({ mutate: vi.fn() }),
}));

vi.mock("@silieco/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@silieco/core/paths")>(
    "@silieco/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

// Module-level flag toggled per-test: desktop implements `openInNewTab`,
// web omits it and the menu has to fall back to a real browser tab.
const { openInNewTabMock, getShareableUrlMock, navState } = vi.hoisted(() => ({
  openInNewTabMock: vi.fn(),
  getShareableUrlMock: vi.fn((p: string) => `https://app.example${p}`),
  navState: { hasOpenInNewTab: true },
}));

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/tasks/task-1",
    searchParams: new URLSearchParams(),
    back: vi.fn(),
    replace: vi.fn(),
    ...(navState.hasOpenInNewTab ? { openInNewTab: openInNewTabMock } : {}),
    getShareableUrl: getShareableUrlMock,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: any) => <span data-testid="actor">{actorId}</span>,
}));

// Import after mocks.
import { TaskActionsDropdown } from "../task-actions-dropdown";
import {
  TaskActionsContextMenu,
  TaskContextMenuProvider,
} from "../task-actions-context-menu";

const mockTask: Task = {
  id: "task-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_task_id: null,
  start_date: null,
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Task;

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  mockOpenModal.mockReset();
  openInNewTabMock.mockReset();
  getShareableUrlMock.mockClear();
  navState.hasOpenInNewTab = true;
});

describe("TaskActionsDropdown", () => {
  it("renders the top-level items when the trigger is clicked", async () => {
    render(
      wrap(
        <TaskActionsDropdown
          task={mockTask}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));

    // Base UI portals the popup; role=menu lands on the popup wrapper.
    expect(await screen.findByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Priority")).toBeInTheDocument();
    expect(screen.getByText("Assignee")).toBeInTheDocument();
    expect(screen.getByText("Due date")).toBeInTheDocument();
    expect(screen.getByText("Open in new tab")).toBeInTheDocument();
    expect(screen.getByText("Copy link")).toBeInTheDocument();
    expect(screen.getByText("Relations")).toBeInTheDocument();
    expect(screen.getByText("Delete task")).toBeInTheDocument();
    // Relationship actions are hidden inside the "Relations" submenu by default.
    expect(screen.queryByText("Create sub-task")).not.toBeInTheDocument();
    expect(screen.queryByText("Set parent task...")).not.toBeInTheDocument();
    expect(screen.queryByText("Add sub-task...")).not.toBeInTheDocument();
  });

  it("clicking the Assignee item opens the shared AssigneePicker popover", async () => {
    render(
      wrap(
        <TaskActionsDropdown
          task={mockTask}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Assignee"));

    // The shared picker exposes a search input and renders the workspace
    // member under a "Members" group — both come from `AssigneePicker`, not
    // the legacy submenu (which had neither).
    expect(
      await screen.findByPlaceholderText("Assign to..."),
    ).toBeInTheDocument();
    expect(await screen.findByText("Members")).toBeInTheDocument();
    expect(await screen.findByText("Test User")).toBeInTheDocument();
  });

  it("shows 'Remove parent task' in the Relations submenu only when the task has a parent", async () => {
    const childTask = { ...mockTask, parent_task_id: "parent-1" } as Task;
    render(
      wrap(
        <TaskActionsDropdown
          task={childTask}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Relations"));

    expect(await screen.findByText("Remove parent task")).toBeInTheDocument();
  });

  it("hides 'Remove parent task' when the task has no parent", async () => {
    render(
      wrap(
        <TaskActionsDropdown
          task={mockTask}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Relations"));

    // The sibling "Set parent task..." proves the submenu opened.
    expect(await screen.findByText("Set parent task...")).toBeInTheDocument();
    expect(screen.queryByText("Remove parent task")).not.toBeInTheDocument();
  });

  it("clicking Delete task opens the delete-confirm modal", async () => {
    render(
      wrap(
        <TaskActionsDropdown
          task={mockTask}
          trigger={<button data-testid="trigger">Menu</button>}
          onDeletedFallbackPath="/test/tasks"
        />,
      ),
    );

    fireEvent.click(screen.getByTestId("trigger"));
    const del = await screen.findByText("Delete task");
    fireEvent.click(del);

    expect(mockOpenModal).toHaveBeenCalledWith("task-delete-confirm", {
      taskId: "task-1",
      identifier: "TES-1",
      onDeletedFallbackPath: "/test/tasks",
    });
  });
});

describe("Open in new tab", () => {
  async function openMenuAndClickOpenInNewTab() {
    render(
      wrap(
        <TaskActionsDropdown
          task={mockTask}
          trigger={<button data-testid="trigger">Menu</button>}
        />,
      ),
    );
    fireEvent.click(screen.getByTestId("trigger"));
    fireEvent.click(await screen.findByText("Open in new tab"));
  }

  it("uses the desktop adapter and focuses the new tab", async () => {
    const windowOpen = vi
      .spyOn(window, "open")
      .mockReturnValue(null as unknown as Window);

    await openMenuAndClickOpenInNewTab();

    // `activate: true` — an explicit CTA moves the user into the new context,
    // unlike modifier-click, which stashes a background tab.
    expect(openInNewTabMock).toHaveBeenCalledWith(
      "/test/tasks/task-1",
      "TES-1",
      { activate: true },
    );
    expect(windowOpen).not.toHaveBeenCalled();

    windowOpen.mockRestore();
  });

  it("falls back to a browser tab when the adapter is absent (web)", async () => {
    navState.hasOpenInNewTab = false;
    const windowOpen = vi
      .spyOn(window, "open")
      .mockReturnValue(null as unknown as Window);

    await openMenuAndClickOpenInNewTab();

    expect(openInNewTabMock).not.toHaveBeenCalled();
    expect(getShareableUrlMock).toHaveBeenCalledWith("/test/tasks/task-1");
    expect(windowOpen).toHaveBeenCalledWith(
      "https://app.example/test/tasks/task-1",
      "_blank",
      "noopener,noreferrer",
    );

    windowOpen.mockRestore();
  });
});

describe("TaskActionsContextMenu", () => {
  it("renders the menu when the wrapped element receives a contextmenu event", async () => {
    render(
      wrap(
        <TaskContextMenuProvider>
          <TaskActionsContextMenu task={mockTask}>
            <div data-testid="row">Row</div>
          </TaskActionsContextMenu>
        </TaskContextMenuProvider>,
      ),
    );

    fireEvent.contextMenu(screen.getByTestId("row"));

    expect(await screen.findByText("Status")).toBeInTheDocument();
    // The right-click surface is what list rows, board cards, gantt bars and
    // sub-task rows all share, so this one assertion covers them together.
    expect(screen.getByText("Open in new tab")).toBeInTheDocument();
    expect(screen.getByText("Delete task")).toBeInTheDocument();
  });
});
