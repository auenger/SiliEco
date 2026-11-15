import { act, render, screen, waitFor } from "@testing-library/react";
import { createRef, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { workspaceKeys } from "@silieco/core/workspace/queries";
import { taskKeys, PAGINATED_STATUSES } from "@silieco/core/tasks/queries";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { TaskStatus, ListTasksCache } from "@silieco/core/types";
import type { QueryClient } from "@tanstack/react-query";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";
import enEditor from "../../locales/en/editor.json";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings, editor: enEditor },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

// Mock the workspace id singleton — items() reads it imperatively.
vi.mock("@silieco/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

// Mock the API so we control search responses + observe calls.
const searchTasksMock = vi.fn();
const searchProjectsMock = vi.fn();
const listFileResourcesMock = vi.fn();
vi.mock("@silieco/core/api", () => ({
  api: {
    get searchTasks() {
      return searchTasksMock;
    },
    get searchProjects() {
      return searchProjectsMock;
    },
    get listFileResources() {
      return listFileResourcesMock;
    },
  },
}));

// Mock the auth store: items() reads `useAuthStore.getState()` imperatively
// to identify the current user when filtering personal agents.
const authState = { user: { id: "u1" } as { id: string } | null };
vi.mock("@silieco/core/auth", () => ({
  useAuthStore: { getState: () => authState },
}));

import {
  createMentionSuggestion,
  MentionList,
  type MentionListRef,
  type MentionItem,
} from "./mention-suggestion";

function fakeQc(data: {
  members?: Array<{ user_id: string; name: string; role?: string }>;
  agents?: Array<{
    id: string;
    name: string;
    archived_at: string | null;
    visibility?: "workspace" | "private";
    owner_id?: string | null;
    permission_mode?: "private" | "public_to";
    invocation_targets?: Array<{
      target_type: "workspace" | "member" | "team";
      target_id: string | null;
    }>;
  }>;
  squads?: Array<{
    id: string;
    name: string;
    archived_at: string | null;
  }>;
  tasks?: Array<{ id: string; identifier: string; title: string; status: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  // SILI-3963: the mention filter runs through canAssignAgentToTask, which
  // reads permission_mode + invocation_targets (not the legacy `visibility`).
  // Fixtures still express intent via `visibility`, so derive the permission
  // fields from it here (public_to + workspace target for "workspace";
  // private + no targets otherwise) unless a fixture sets them explicitly.
  const agentsWithPermissions = (data.agents ?? []).map((a) => ({
    ...a,
    permission_mode:
      a.permission_mode ?? (a.visibility === "private" ? "private" : "public_to"),
    invocation_targets:
      a.invocation_targets ??
      (a.visibility === "private"
        ? []
        : [{ target_type: "workspace" as const, target_id: null }]),
  }));
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), agentsWithPermissions);
  map.set(JSON.stringify(workspaceKeys.squads("ws-1")), data.squads ?? []);
  const byStatus: ListTasksCache["byStatus"] = {};
  for (const status of PAGINATED_STATUSES) {
    const bucket = (data.tasks ?? []).filter((i) => i.status === status);
    byStatus[status as TaskStatus] = { tasks: bucket as never, total: bucket.length };
  }
  map.set(
    JSON.stringify(taskKeys.list("ws-1")),
    { byStatus } satisfies ListTasksCache,
  );
  return {
    getQueryData: (key: readonly unknown[]) => map.get(JSON.stringify(key)),
    getQueriesData: <T,>(filter: { queryKey: readonly unknown[] }) => {
      const prefix = filter.queryKey as unknown[];
      const results: [readonly unknown[], T][] = [];
      for (const [k, v] of map) {
        const parsed = JSON.parse(k) as unknown[];
        if (parsed.length >= prefix.length && prefix.every((seg, i) => JSON.stringify(seg) === JSON.stringify(parsed[i]))) {
          results.push([parsed, v as T]);
        }
      }
      return results;
    },
  } as unknown as QueryClient;
}

function itemArgs(query: string) {
  return {
    query,
    editor: {} as never,
    signal: new AbortController().signal,
  };
}

describe("createMentionSuggestion", () => {
  beforeEach(() => {
    searchTasksMock.mockReset();
    searchProjectsMock.mockReset();
    listFileResourcesMock.mockReset();
    listFileResourcesMock.mockResolvedValue({ files: [], total: 0 });
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("keeps the mention query active across spaces for multi-word search", () => {
    const qc = fakeQc({
      tasks: [
        {
          id: "i-login",
          identifier: "SILI-1",
          title: "Login redirect bug",
          status: "todo",
        },
      ],
    });
    const config = createMentionSuggestion(qc);

    expect(config.allowSpaces).toBe(true);
    expect(config.items!(itemArgs("login redirect"))).toEqual([
      expect.objectContaining({ id: "i-login", type: "task" }),
    ]);
  });

  it("returns members and agents synchronously without waiting for the server search", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [
        {
          id: "a1",
          name: "Aegis",
          archived_at: null,
          visibility: "workspace",
          owner_id: null,
        },
      ],
    });
    // A pending fetch — would block the result if items() awaited it.
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("a"));

    // Must be synchronous: a plain array, not a Promise.
    expect(Array.isArray(result)).toBe(true);
    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "Alice")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aegis")).toBe(true);
  });

  it("loads server task matches into the popup when the list cache misses", async () => {
    searchTasksMock.mockResolvedValue({
      tasks: [
        {
          id: "i-1007",
          identifier: "SILI-1007",
          title: "多 Agent 协作探索",
          status: "done",
        },
      ],
      total: 1,
    });

    render(<I18nWrapper><MentionList items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(screen.getByText("Searching...")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText("SILI-1007")).toBeInTheDocument();
    });
    expect(screen.getByText("多 Agent 协作探索")).toBeInTheDocument();
    expect(searchTasksMock).toHaveBeenCalledWith(
      expect.objectContaining({
        q: "协作",
        limit: 20,
        include_closed: true,
      }),
    );
  });

  it("loads server task and project matches when project search is enabled", async () => {
    searchTasksMock.mockResolvedValue({ tasks: [], total: 0 });
    searchProjectsMock.mockResolvedValue({
      projects: [
        {
          id: "p-roadmap",
          title: "Roadmap",
          description: "Q3 planning",
          icon: null,
          status: "active",
        },
      ],
      total: 1,
    });

    render(
      <I18nWrapper>
        <MentionList items={[]} query="road" command={vi.fn()} includeProjectSearch />
      </I18nWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Roadmap")).toBeInTheDocument();
    });
    expect(searchTasksMock).toHaveBeenCalledWith(expect.objectContaining({ q: "road", limit: 8 }));
    expect(searchProjectsMock).toHaveBeenCalledWith(expect.objectContaining({ q: "road", limit: 8 }));
  });

  it("does not call searchTasks for an empty query", () => {
    render(<I18nWrapper><MentionList items={[]} query="" command={vi.fn()} /></I18nWrapper>);

    expect(searchTasksMock).not.toHaveBeenCalled();
    expect(searchProjectsMock).not.toHaveBeenCalled();
  });

  it("loads Space resource files into their own @ picker section", async () => {
    listFileResourcesMock.mockResolvedValue({
      files: [{
        id: "file-1",
        filename: "wireframe.png",
        content_type: "image/png",
        url: "/uploads/wireframe.png",
        download_url: "/api/attachments/file-1/download",
        markdown_url: "http://localhost:8080/api/attachments/file-1/download",
        source_project_title: "Website",
        source_task_title: "Landing page",
      }],
      total: 1,
    });
    const command = vi.fn();
    const onSelectResource = vi.fn();

    render(
      <I18nWrapper>
        <MentionList
          items={[]}
          query="wire"
          command={command}
          includeResources
          onSelectResource={onSelectResource}
        />
      </I18nWrapper>,
    );

    await waitFor(() => expect(screen.getByText("wireframe.png")).toBeInTheDocument());
    expect(screen.getByText("Resource files")).toBeInTheDocument();
    expect(screen.getByText("Website · Landing page")).toBeInTheDocument();

    screen.getByText("wireframe.png").closest("button")?.click();
    expect(onSelectResource).toHaveBeenCalledWith(expect.objectContaining({ type: "file", id: "file-1" }));
    expect(command).not.toHaveBeenCalled();
  });

  it("captures Enter while the popup has no selectable items", () => {
    const ref = createRef<MentionListRef>();

    render(<I18nWrapper><MentionList ref={ref} items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) }),
    ).toBe(true);
  });

  // SILI-3685: plain Tab accepts the highlighted row exactly like Enter.
  it("accepts the highlighted row on plain Tab, like Enter", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [
      { id: "i-1", label: "SILI-1", type: "task" },
      { id: "i-2", label: "SILI-2", type: "task" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} />
      </I18nWrapper>,
    );

    const handled = ref.current?.onKeyDown({
      event: new KeyboardEvent("keydown", { key: "Tab" }),
    });

    expect(handled).toBe(true);
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("SILI-1");
  });

  // Shift+Tab and any modifier+Tab stay focus navigation — they must NOT
  // accept, so the picker never traps reverse Tab traversal or OS switching.
  it("does not accept on Shift+Tab or modifier+Tab", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [{ id: "i-1", label: "SILI-1", type: "task" }];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} />
      </I18nWrapper>,
    );

    const press = (init: KeyboardEventInit) =>
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", init) });

    expect(press({ key: "Tab", shiftKey: true })).toBe(false);
    expect(press({ key: "Tab", metaKey: true })).toBe(false);
    expect(press({ key: "Tab", ctrlKey: true })).toBe(false);
    expect(press({ key: "Tab", altKey: true })).toBe(false);
    expect(command).not.toHaveBeenCalled();
  });

  it("captures Tab while the popup has no selectable items, like Enter", () => {
    const ref = createRef<MentionListRef>();

    render(<I18nWrapper><MentionList ref={ref} items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Tab" }) }),
    ).toBe(true);
  });

  // SILI-3607: groupItems() re-buckets the list (current → recent → search →
  // users → tasks), so an item that sits LATER in the data array can render
  // NEAR THE TOP. Selection must follow the rendered order — otherwise the
  // highlighted row and the committed item drift apart and you mention the
  // neighbour of who you picked. (Task rows are used because they render
  // without workspace/avatar context; the bug is type-agnostic.)
  it("commits the highlighted row, not its neighbour, when groups reorder the list", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();

    // Data order is [SILI-2 (tasks bucket), SILI-1 (search bucket)], but
    // groupItems hoists the search row, so the RENDERED order is [SILI-1, SILI-2].
    const items: MentionItem[] = [
      { id: "i-plain", label: "SILI-2", type: "task" },
      { id: "i-search", label: "SILI-1", type: "task", group: "search" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} includeProjectSearch />
      </I18nWrapper>,
    );

    const highlightedLabel = () => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
      return buttons.find((b) => b.classList.contains("bg-accent"))?.textContent ?? "";
    };
    const press = (key: string) =>
      act(() => {
        ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key }) });
      });

    // First rendered row is the hoisted search result. Enter commits it, not
    // the task that sits first in the data array.
    expect(highlightedLabel()).toBe("SILI-1");
    press("Enter");
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("SILI-1");

    command.mockClear();

    // Arrow down one row, then Enter — still commits exactly the highlighted row.
    press("ArrowDown");
    expect(highlightedLabel()).toBe("SILI-2");
    press("Enter");
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("SILI-2");
  });

  it("hides personal agents owned by someone else from a regular member", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "Bob", role: "member" },
      ],
      agents: [
        // Bob's personal agent — Alice (current user) should not see it.
        {
          id: "a-personal-bob",
          name: "Atlas",
          archived_at: null,
          visibility: "private",
          owner_id: "u2",
        },
        // Alice's own personal agent — should be visible.
        {
          id: "a-personal-alice",
          name: "Athena",
          archived_at: null,
          visibility: "private",
          owner_id: "u1",
        },
        // Workspace agent — visible to everyone.
        {
          id: "a-shared",
          name: "Aether",
          archived_at: null,
          visibility: "workspace",
          owner_id: "u2",
        },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("a"));
    const items = result as MentionItem[];

    expect(items.some((i) => i.type === "agent" && i.label === "Athena")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aether")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
  });

  it("hides another owner's personal agent from a workspace admin (SILI-3963)", () => {
    // Role lives in the member fixture, not in authState — promoting Alice
    // to admin here exercises the gate. SILI-3963 removed the admin bypass:
    // a private agent is invocable only by its owner, so the @mention list
    // must NOT surface Bob's personal agent to admin Alice.
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "admin" },
        { user_id: "u2", name: "Bob", role: "member" },
      ],
      agents: [
        {
          id: "a-personal-bob",
          name: "Atlas",
          archived_at: null,
          visibility: "private",
          owner_id: "u2",
        },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("a"));
    const items = result as MentionItem[];

    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
  });

  it("includes cached tasks in the synchronous response", () => {
    const qc = fakeQc({
      tasks: [
        { id: "i1", identifier: "SILI-1", title: "Login bug", status: "todo" },
        { id: "i2", identifier: "SILI-2", title: "Other", status: "done" },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("bug"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "task" && i.id === "i1")).toBe(true);
  });

  it("does not inject current/recent chat context into the normal @ results", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      tasks: [{ id: "i1", identifier: "SILI-1", title: "Login bug", status: "todo" }],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("")) as MentionItem[];

    expect(result.some((item) => item.group === "current" || item.group === "recent")).toBe(false);
    expect(result.map((item) => `${item.type}:${item.id}`)).toContain("member:u1");
    expect(result.map((item) => `${item.type}:${item.id}`)).toContain("task:i1");
  });


  it("shows only current/recent chat context before the user types a query", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [{ id: "a1", name: "Aegis", archived_at: null, visibility: "workspace", owner_id: null }],
      tasks: [{ id: "i-cache", identifier: "SILI-9", title: "Cached", status: "todo" }],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc, {
      mode: "context",
      getContextItems: () => [
        { id: "i1", label: "SILI-1", type: "task", description: "Alpha task", status: "todo", group: "current" },
        { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
      ],
    });
    const result = config.items!(itemArgs("")) as MentionItem[];

    expect(result.map((item) => `${item.type}:${item.id}`)).toEqual(["task:i1", "project:p1"]);
    expect(result.some((item) => item.type === "member" || item.type === "agent")).toBe(false);
  });

  it("prepends current/recent chat context without removing normal mention targets after the user types", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [{ id: "a1", name: "Aegis", archived_at: null, visibility: "workspace", owner_id: null }],
      tasks: [{ id: "i-cache", identifier: "SILI-9", title: "Cached", status: "todo" }],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc, {
      mode: "context",
      getContextItems: () => [
        { id: "i1", label: "SILI-1", type: "task", description: "Alpha task", status: "todo", group: "current" },
        { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
      ],
    });
    const result = config.items!(itemArgs("a")) as MentionItem[];

    expect(result.map((item) => `${item.type}:${item.id}`).slice(0, 2)).toEqual(["task:i1", "project:p1"]);
    expect(result.some((item) => item.type === "member" && item.label === "Alice")).toBe(true);
    expect(result.some((item) => item.type === "agent" && item.label === "Aegis")).toBe(true);
  });

  it("renders current and recent sections for injected object mentions", () => {
    render(
      <I18nWrapper>
        <MentionList
          items={[
            { id: "i1", label: "SILI-1", type: "task", description: "Login bug", group: "current" },
            { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
          ]}
          query=""
          command={vi.fn()}
        />
      </I18nWrapper>,
    );

    expect(screen.getByText("Current page")).toBeInTheDocument();
    expect(screen.getByText("Recently viewed")).toBeInTheDocument();
    expect(screen.getByText("SILI-1")).toBeInTheDocument();
    expect(screen.getByText("Roadmap")).toBeInTheDocument();
  });

  it("includes all non-archived squads in the mention list", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      squads: [
        { id: "s1", name: "Jiayuan's Coding Team", archived_at: null },
        { id: "s2", name: "独立团", archived_at: null },
        { id: "s3", name: "Archived Squad", archived_at: "2026-01-01T00:00:00Z" },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs(""));

    const items = result as MentionItem[];
    expect(items.filter((i) => i.type === "squad")).toHaveLength(2);
    expect(items.some((i) => i.type === "squad" && i.label === "Jiayuan's Coding Team")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "独立团")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "Archived Squad")).toBe(false);
  });

  it("returns no squads when the squads cache is empty (not yet fetched)", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      // squads not provided — simulates cache miss
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs(""));

    const items = result as MentionItem[];
    expect(items.filter((i) => i.type === "squad")).toHaveLength(0);
  });

  it("matches Chinese names by full pinyin", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "李云龙", role: "member" },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("liyunlong"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "李云龙")).toBe(true);
    expect(items.some((i) => i.type === "member" && i.label === "Alice")).toBe(false);
  });

  it("matches Chinese names by pinyin initials", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "李云龙", role: "member" },
        { user_id: "u3", name: "张大彪", role: "member" },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("lyl"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "李云龙")).toBe(true);
    expect(items.some((i) => i.type === "member" && i.label === "张大彪")).toBe(false);
  });

  it("matches Chinese agent names by pinyin", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [
        { id: "a1", name: "魏和尚", archived_at: null, visibility: "workspace", owner_id: null },
      ],
    });
    searchTasksMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("whs"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "agent" && i.label === "魏和尚")).toBe(true);
  });
});
