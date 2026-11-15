import { act, type ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";
import enCommon from "../locales/en/common.json";
import enAuth from "../locales/en/auth.json";
import enSettings from "../locales/en/settings.json";
import enSearch from "../locales/en/search.json";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings, search: enSearch },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const renderSearch = () => render(<SearchCommand />, { wrapper: I18nWrapper });

const {
  mockPush,
  mockSearchTasks,
  mockSearchProjects,
  mockRecentItems,
  mockAllTasks,
  mockSetTheme,
  mockTheme,
  mockPathname,
  mockGetShareableUrl,
  mockMembers,
  mockAgents,
  mockSquads,
  mockOpenModal,
  mockToastSuccess,
  mockClipboardWrite,
  mockTimeline,
  mockCommentCollapseAll,
  mockCommentExpandAll,
  mockResolvedCollapseAll,
  mockResolvedExpandAll,
} = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchTasks: vi.fn(),
  mockSearchProjects: vi.fn(),
  mockRecentItems: { current: [] as Array<{ id: string; visitedAt: number }> },
  mockAllTasks: { current: [] as Array<Record<string, unknown>> },
  mockSetTheme: vi.fn(),
  mockTheme: { current: "system" as "light" | "dark" | "system" },
  mockPathname: { current: "/ws-test/tasks" as string },
  mockGetShareableUrl: vi.fn((p: string) => `https://app.silieco/${p}`),
  mockMembers: {
    current: [] as Array<{
      id: string;
      workspace_id: string;
      user_id: string;
      role: "owner" | "admin" | "member";
      created_at: string;
      name: string;
      email: string;
      avatar_url: string | null;
    }>,
  },
  mockAgents: {
    current: [] as Array<{
      id: string;
      name: string;
      avatar_url: string | null;
    }>,
  },
  mockSquads: {
    current: [] as Array<{
      id: string;
      name: string;
      avatar_url: string | null;
    }>,
  },
  mockOpenModal: vi.fn(),
  mockToastSuccess: vi.fn(),
  mockClipboardWrite: vi.fn(() => Promise.resolve()),
  mockTimeline: { current: [] as Array<Record<string, unknown>> },
  mockCommentCollapseAll: vi.fn(),
  mockCommentExpandAll: vi.fn(),
  mockResolvedCollapseAll: vi.fn(),
  mockResolvedExpandAll: vi.fn(),
}));

vi.mock("@silieco/core/api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
    searchTasks: mockSearchTasks,
    searchProjects: mockSearchProjects,
  },
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({
    actorType,
    actorId,
  }: {
    actorType: string;
    actorId: string;
  }) => {
    const name =
      actorType === "member"
        ? mockMembers.current.find((m) => m.user_id === actorId)?.name
        : actorType === "agent"
          ? mockAgents.current.find((a) => a.id === actorId)?.name
          : actorType === "squad"
            ? mockSquads.current.find((s) => s.id === actorId)?.name
            : undefined;
    return (
      <span
        data-testid="task-assignee-avatar"
        title={name ?? `${actorType}:${actorId}`}
      />
    );
  },
}));

vi.mock("@silieco/core/tasks/stores", () => {
  const EMPTY: Array<{ id: string; visitedAt: number }> = [];
  return {
    useRecentTasksStore: (
      selector?: (state: {
        byWorkspace: Record<string, typeof mockRecentItems.current>;
      }) => unknown,
    ) => {
      const state = { byWorkspace: { "ws-test": mockRecentItems.current } };
      return selector ? selector(state) : state;
    },
    selectRecentTasks:
      (wsId: string | null) =>
      (state: { byWorkspace: Record<string, typeof mockRecentItems.current> }) =>
        wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY,
    openCreateTaskWithPreference: (data?: Record<string, unknown> | null) =>
      mockOpenModal("quick-create-task", data ?? null),
    useCommentCollapseStore: Object.assign(vi.fn(), {
      getState: () => ({
        collapseAll: mockCommentCollapseAll,
        expandAll: mockCommentExpandAll,
      }),
    }),
    useResolvedExpandStore: Object.assign(vi.fn(), {
      getState: () => ({
        collapseAll: mockResolvedCollapseAll,
        expandAll: mockResolvedExpandAll,
      }),
    }),
  };
});

vi.mock("@silieco/core", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@silieco/core/paths", async (importOriginal) => ({
  // Spread the real module so pure helpers (resolveRouteIconName, used to
  // derive each nav page's icon from its href) stay intact.
  ...(await importOriginal<typeof import("@silieco/core/paths")>()),
  useWorkspacePaths: () => ({
    inbox: () => "/ws-test/inbox",
    myTasks: () => "/ws-test/my-tasks",
    tasks: () => "/ws-test/tasks",
    projects: () => "/ws-test/projects",
    agents: () => "/ws-test/agents",
    runtimes: () => "/ws-test/runtimes",
    skills: () => "/ws-test/skills",
    settings: () => "/ws-test/settings",
    taskDetail: (id: string) => `/ws-test/tasks/${id}`,
    memberDetail: (id: string) => `/ws-test/members/${id}`,
    agentDetail: (id: string) => `/ws-test/agents/${id}`,
    squadDetail: (id: string) => `/ws-test/squads/${id}`,
    projectDetail: (id: string) => `/ws-test/projects/${id}`,
  }),
}));

vi.mock("@silieco/core/tasks/queries", () => ({
  taskDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["tasks", "ws-test", "detail", id],
  }),
  taskTimelineOptions: (id: string) => ({
    queryKey: ["tasks", "timeline", id],
  }),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspaces", "ws-test", "members"] }),
  agentListOptions: () => ({ queryKey: ["workspaces", "ws-test", "agents"] }),
  squadListOptions: () => ({ queryKey: ["workspaces", "ws-test", "squads"] }),
}));

vi.mock("@silieco/core/modals", () => ({
  useModalStore: Object.assign(vi.fn(), {
    getState: () => ({ open: mockOpenModal }),
  }),
}));

function resolveTask(key: readonly unknown[]) {
  // taskDetailOptions key shape: ["tasks", wsId, "detail", id]
  if (key[0] === "tasks" && key[2] === "detail") {
    const id = key[3];
    return mockAllTasks.current.find((i) => i.id === id);
  }
  return undefined;
}

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    const key = opts.queryKey;
    if (key[0] === "workspaces" && key[2] === "members") {
      return { data: mockMembers.current };
    }
    if (key[0] === "workspaces" && key[2] === "agents") {
      return { data: mockAgents.current };
    }
    if (key[0] === "workspaces" && key[2] === "squads") {
      return { data: mockSquads.current };
    }
    if (opts.enabled === false) return { data: undefined };
    return { data: resolveTask(key) };
  },
  useQueries: (opts: { queries: Array<{ queryKey: readonly unknown[] }> }) =>
    opts.queries.map((q) => ({ data: resolveTask(q.queryKey) })),
  useQueryClient: () => ({
    ensureQueryData: (opts: { queryKey: readonly unknown[] }) =>
      opts.queryKey[1] === "timeline"
        ? Promise.resolve(mockTimeline.current)
        : Promise.reject(new Error(`unexpected key ${String(opts.queryKey)}`)),
  }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: mockPush,
    pathname: mockPathname.current,
    getShareableUrl: mockGetShareableUrl,
  }),
}));

vi.mock("@silieco/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: mockTheme.current, setTheme: mockSetTheme }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn() },
}));

describe("SearchCommand", () => {
  beforeEach(() => {
    mockPush.mockReset();
    mockSearchTasks.mockReset().mockResolvedValue({ tasks: [] });
    mockSearchProjects.mockReset().mockResolvedValue({ projects: [] });
    mockRecentItems.current = [];
    mockAllTasks.current = [];
    mockAgents.current = [];
    mockSquads.current = [];
    mockSetTheme.mockReset();
    mockTheme.current = "system";
    mockPathname.current = "/ws-test/tasks";
    mockGetShareableUrl.mockReset().mockImplementation((p: string) => `https://app.silieco/${p}`);
    mockMembers.current = [];
    mockOpenModal.mockReset();
    mockToastSuccess.mockReset();
    mockClipboardWrite.mockReset().mockResolvedValue(undefined);
    mockTimeline.current = [];
    mockCommentCollapseAll.mockReset();
    mockCommentExpandAll.mockReset();
    mockResolvedCollapseAll.mockReset();
    mockResolvedExpandAll.mockReset();

    // cmdk calls scrollIntoView on the first selected item, which jsdom doesn't implement
    Element.prototype.scrollIntoView = vi.fn();

    act(() => {
      useSearchStore.setState({ open: true });
    });
  });

  it("closes on a single Escape press from the search input", async () => {
    const user = userEvent.setup();

    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.click(input);

    expect(useSearchStore.getState().open).toBe(true);

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(useSearchStore.getState().open).toBe(false);
    });
    expect(screen.queryByPlaceholderText("Type a command or search...")).not.toBeInTheDocument();
  });

  it("shows only New Task by default and hides Pages / low-frequency commands until query", () => {
    renderSearch();

    expect(screen.queryByText("Pages")).not.toBeInTheDocument();
    // Only the primary creation action surfaces on empty query; everything
    // else (theme, copy, New Project) must be revealed by typing.
    expect(screen.getByText("Commands")).toBeInTheDocument();
    expect(
      screen.getByText((_, el) => el?.textContent === "New Task" && el?.tagName === "SPAN"),
    ).toBeInTheDocument();
    expect(screen.queryByText("New Project")).not.toBeInTheDocument();
    expect(screen.queryByText("Switch to Light Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Switch to Dark Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Use System Theme")).not.toBeInTheDocument();
  });

  it("filters navigation pages by query", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "set");

    await waitFor(() => {
      // HighlightText splits text, so use a function matcher
      expect(screen.getByText((_, el) => el?.textContent === "Settings" && el?.tagName === "SPAN")).toBeInTheDocument();
    });
    expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
  });

  it("navigates to page on selection", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "settings");

    const settingsItem = await screen.findByText("Settings");
    await user.click(settingsItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("lists workspace members and navigates to the member page on selection", async () => {
    const user = userEvent.setup();
    mockMembers.current = [
      {
        id: "member-1",
        workspace_id: "ws-test",
        user_id: "user-1",
        role: "member",
        created_at: "2026-01-01T00:00:00Z",
        name: "Alice Zhang",
        email: "alice@example.com",
        avatar_url: null,
      },
      {
        id: "member-2",
        workspace_id: "ws-test",
        user_id: "user-2",
        role: "admin",
        created_at: "2026-01-01T00:00:00Z",
        name: "Bob Liu",
        email: "bob@example.com",
        avatar_url: null,
      },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "alice");

    await waitFor(() => {
      expect(screen.getByText("Members")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Alice Zhang" && el?.tagName === "DIV"),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByText((_, el) => el?.textContent === "alice@example.com" && el?.tagName === "DIV"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Bob Liu")).not.toBeInTheDocument();

    const aliceItem = await screen.findByText(
      (_, el) => el?.textContent === "Alice Zhang" && el?.tagName === "DIV",
    );
    await user.click(aliceItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/members/user-1");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("renders recent tasks from query cache joined with store visit records", () => {
    mockRecentItems.current = [
      { id: "task-1", visitedAt: 1000 },
      { id: "task-2", visitedAt: 900 },
    ];
    mockAllTasks.current = [
      { id: "task-1", identifier: "SILI-1", title: "First task", status: "todo" },
      { id: "task-2", identifier: "SILI-2", title: "Second task", status: "done" },
    ];

    renderSearch();

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("First task")).toBeInTheDocument();
    expect(screen.getByText("SILI-1")).toBeInTheDocument();
    expect(screen.getByText("Second task")).toBeInTheDocument();
    expect(screen.getByText("SILI-2")).toBeInTheDocument();
  });

  it("shows New Task / New Project under Commands and triggers the modal store", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "new");

    await waitFor(() => {
      expect(screen.getByText("Commands")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Task" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Project" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });

    const newTask = await screen.findByText(
      (_, el) => el?.textContent === "New Task" && el?.tagName === "SPAN",
    );
    await user.click(newTask);

    expect(mockOpenModal).toHaveBeenCalledWith("quick-create-task", null);
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("hides copy-link commands when not on an task detail route", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/projects";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "copy");

    // Commands section may still be empty / absent.
    expect(screen.queryByText("Copy Task Link")).not.toBeInTheDocument();
  });

  it("copies task link and identifier when on an task detail route", async () => {
    const user = userEvent.setup();
    // userEvent.setup() installs its own navigator.clipboard; spy on it so we
    // intercept the writeText call without clobbering userEvent's internals.
    const writeSpy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockImplementation(mockClipboardWrite);
    mockPathname.current = "/ws-test/tasks/task-1";
    mockAllTasks.current = [
      { id: "task-1", identifier: "SILI-42", title: "Demo", status: "todo" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "copy");

    const linkItem = await screen.findByText(
      (_, el) => el?.textContent === "Copy Task Link" && el?.tagName === "SPAN",
    );
    await user.click(linkItem);

    expect(mockGetShareableUrl).toHaveBeenCalledWith("/ws-test/tasks/task-1");
    expect(mockClipboardWrite).toHaveBeenCalledWith("https://app.silieco//ws-test/tasks/task-1");
    expect(mockToastSuccess).toHaveBeenCalledWith("Link copied");

    // Reopen palette and test identifier copy
    act(() => {
      useSearchStore.setState({ open: true });
    });
    const input2 = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input2, "copy");
    const idItem = await screen.findByText(
      (_, el) =>
        el?.textContent === "Copy Identifier (SILI-42)" && el?.tagName === "SPAN",
    );
    await user.click(idItem);
    expect(mockClipboardWrite).toHaveBeenCalledWith("SILI-42");
    expect(mockToastSuccess).toHaveBeenCalledWith("Copied SILI-42");

    writeSpy.mockRestore();
  });

  it("hides fold/unfold-all-comments commands off task detail routes", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/projects";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "fold");

    expect(screen.queryByText("Fold All Comments")).not.toBeInTheDocument();
    expect(screen.queryByText("Unfold All Comments")).not.toBeInTheDocument();
  });

  it("folds all comments on the current task, including expanded resolved threads", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/tasks/task-1";
    mockAllTasks.current = [
      { id: "task-1", identifier: "SILI-42", title: "Demo", status: "todo" },
    ];
    mockTimeline.current = [
      { type: "activity", id: "act-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T00:00:00Z", action: "status_changed" },
      { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
      { type: "comment", id: "reply-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T02:00:00Z", parent_id: "root-1" },
      { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "fold");

    const foldItem = await screen.findByText(
      (_, el) => el?.textContent === "Fold All Comments" && el?.tagName === "SPAN",
    );
    await user.click(foldItem);

    await waitFor(() => {
      // Root comments only — replies fold with their thread, activities are not comments.
      expect(mockCommentCollapseAll).toHaveBeenCalledWith("task-1", ["root-1", "root-2"]);
    });
    expect(mockResolvedCollapseAll).toHaveBeenCalledWith("task-1");
    expect(mockCommentExpandAll).not.toHaveBeenCalled();
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("unfolds all comments and expands resolved threads", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/tasks/task-1";
    mockAllTasks.current = [
      { id: "task-1", identifier: "SILI-42", title: "Demo", status: "todo" },
    ];
    mockTimeline.current = [
      { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
      { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
      // root-3 is reply-resolved: the resolution lives on the reply.
      { type: "comment", id: "root-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T04:00:00Z", parent_id: null },
      { type: "comment", id: "reply-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T05:00:00Z", parent_id: "root-3", resolved_at: "2026-01-02T01:00:00Z" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "unfold");

    const unfoldItem = await screen.findByText(
      (_, el) => el?.textContent === "Unfold All Comments" && el?.tagName === "SPAN",
    );
    await user.click(unfoldItem);

    await waitFor(() => {
      expect(mockCommentExpandAll).toHaveBeenCalledWith("task-1");
    });
    // Only threads carrying a resolution get seeded into the expand set.
    expect(mockResolvedExpandAll).toHaveBeenCalledWith("task-1", ["root-2", "root-3"]);
    expect(mockCommentCollapseAll).not.toHaveBeenCalled();
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("filters theme commands by query keywords", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "dark");

    await waitFor(() => {
      expect(screen.getByText("Commands")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Switch to Light Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Use System Theme")).not.toBeInTheDocument();
  });

  it("applies the selected theme and closes the palette", async () => {
    const user = userEvent.setup();
    mockTheme.current = "light";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "dark");

    const darkItem = await screen.findByText(
      (_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN",
    );
    await user.click(darkItem);

    expect(mockSetTheme).toHaveBeenCalledWith("dark");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("matches theme action via generic 'theme' keyword and marks current theme", async () => {
    const user = userEvent.setup();
    mockTheme.current = "dark";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "theme");

    await waitFor(() => {
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Light Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Use System Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.getByLabelText("Current theme")).toBeInTheDocument();
  });

  it("filters out recent items not present in query cache", () => {
    mockRecentItems.current = [
      { id: "task-1", visitedAt: 1000 },
      { id: "deleted-task", visitedAt: 900 },
    ];
    mockAllTasks.current = [
      { id: "task-1", identifier: "SILI-1", title: "Existing task", status: "in_progress" },
    ];

    renderSearch();

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("Existing task")).toBeInTheDocument();
    expect(screen.queryByText("deleted-task")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar instead of status text for task search results", async () => {
    const user = userEvent.setup();
    mockMembers.current = [
      {
        id: "member-1",
        workspace_id: "ws-test",
        user_id: "user-1",
        role: "member",
        created_at: "2026-01-01T00:00:00Z",
        name: "Alice Zhang",
        email: "alice@example.com",
        avatar_url: null,
      },
    ];
    mockSearchTasks.mockResolvedValue({
      tasks: [
        {
          id: "task-assigned",
          workspace_id: "ws-test",
          number: 101,
          identifier: "SILI-101",
          title: "Assigned search result",
          description: null,
          status: "in_review",
          priority: "none",
          assignee_type: "member",
          assignee_id: "user-1",
          creator_type: "member",
          creator_id: "user-1",
          parent_task_id: null,
          project_id: null,
          position: 0,
          start_date: null,
          due_date: null,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          match_source: "title",
        },
      ],
      total: 1,
    });

    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "assigned");

    await waitFor(
      () => {
        expect(
          screen.getByText((_, el) =>
            el?.textContent === "Assigned search result" &&
            el?.tagName === "SPAN",
          ),
        ).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    expect(screen.getByTitle("Alice Zhang")).toBeInTheDocument();
    expect(screen.queryByText("In Review")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar instead of status text for recent tasks", () => {
    mockRecentItems.current = [{ id: "task-1", visitedAt: 1000 }];
    mockAgents.current = [{ id: "agent-1", name: "Niko", avatar_url: null }];
    mockAllTasks.current = [
      {
        id: "task-1",
        identifier: "SILI-1",
        title: "Recent assigned task",
        status: "done",
        assignee_type: "agent",
        assignee_id: "agent-1",
      },
    ];

    renderSearch();

    expect(screen.getByText("Recent assigned task")).toBeInTheDocument();
    expect(screen.getByTitle("Niko")).toBeInTheDocument();
    expect(screen.queryByText("Done")).not.toBeInTheDocument();
  });

  it("renders description and comment snippets regardless of match_source", async () => {
    const user = userEvent.setup();
    mockSearchTasks.mockResolvedValue({
      tasks: [
        {
          id: "task-snippet",
          workspace_id: "ws-test",
          number: 99,
          identifier: "SILI-99",
          title: "HTML rendering pipeline",
          description: null,
          status: "todo",
          priority: "none",
          assignee_type: null,
          assignee_id: null,
          creator_type: "member",
          creator_id: "user-1",
          parent_task_id: null,
          project_id: null,
          position: 0,
          start_date: null,
          due_date: null,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          match_source: "title",
          matched_description_snippet: "...uses HTML templates for rendering...",
          matched_comment_snippet: "...we should migrate away from HTML...",
        },
      ],
      total: 1,
    });
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "html");

    await waitFor(
      () => {
        expect(screen.getByText((_, el) => el?.textContent === "HTML rendering pipeline" && el?.tagName === "SPAN")).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    // Description snippet should render even though match_source is "title"
    expect(
      screen.getByText((_, el) =>
        (el?.textContent?.includes("uses HTML templates for rendering") ?? false) &&
        el?.tagName === "SPAN",
      ),
    ).toBeInTheDocument();

    // Comment snippet should render even though match_source is "title"
    expect(
      screen.getByText((_, el) =>
        (el?.textContent?.includes("we should migrate away from HTML") ?? false) &&
        el?.tagName === "SPAN",
      ),
    ).toBeInTheDocument();
  });

  it("lets Home/End edit the query caret instead of hijacking result navigation", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText(
      "Type a command or search...",
    ) as HTMLInputElement;
    await user.type(input, "new");

    // "new" surfaces New Task + New Project, so cmdk has a multi-item list.
    await waitFor(() => {
      expect(
        screen.getByText((_, el) => el?.textContent === "New Task" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Project" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });

    const selectedValue = () =>
      document
        .querySelector('[cmdk-item=""][aria-selected="true"]')
        ?.getAttribute("data-value") ?? null;

    // cmdk auto-selects the first item; Arrow keys must still move that selection.
    const first = selectedValue();
    expect(first).not.toBeNull();

    await user.keyboard("{ArrowDown}");
    expect(selectedValue()).not.toBe(first);

    await user.keyboard("{ArrowUp}");
    expect(selectedValue()).toBe(first);

    // Home moves the text caret to the start — it must NOT jump the result list.
    await user.keyboard("{Home}");
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(0);
    expect(selectedValue()).toBe(first);

    // End moves the caret to the end — again leaving the result selection alone.
    await user.keyboard("{End}");
    expect(input.selectionStart).toBe(input.value.length);
    expect(input.selectionEnd).toBe(input.value.length);
    expect(selectedValue()).toBe(first);
  });
});
