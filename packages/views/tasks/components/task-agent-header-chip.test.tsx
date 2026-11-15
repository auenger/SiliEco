// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentRun } from "@silieco/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  runs: [] as unknown[],
  agentRunMessagesOptions: vi.fn(),
  // Captures the props the chip passes to PopoverTrigger so a test can assert
  // the card is wired to open on hover, not only on click.
  triggerProps: undefined as Record<string, unknown> | undefined,
}));

vi.mock("@silieco/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@silieco/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({
        "agent-1": "Walt",
        "agent-2": "Gus",
      })[id] ?? "Unknown Agent",
    getActorInitials: (_type: string, id: string) =>
      ({
        "agent-1": "WA",
        "agent-2": "GU",
      })[id] ?? "UA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@silieco/core/chat/queries", () => ({
  agentRunMessagesOptions: mockState.agentRunMessagesOptions,
}));

vi.mock("@silieco/ui/components/ui/popover", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    Popover: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="agent-popover">{children}</div>
    ),
    PopoverTrigger: ({
      render,
      children,
      ...props
    }: {
      render: React.ReactElement;
      children: React.ReactNode;
    } & Record<string, unknown>) => {
      mockState.triggerProps = props;
      return React.cloneElement(render, undefined, children);
    },
    PopoverContent: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="agent-popover-content">{children}</div>
    ),
  };
});

vi.mock("./execution-log-section", () => ({
  ActiveRunRow: ({ run }: { run: AgentRun }) => (
    <div data-testid="active-task-row">{run.id}</div>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );

  return {
    ...actual,
    useQuery: (opts: { queryKey?: readonly unknown[] }) => {
      // Per-task task list: agentRunsKeys.forTask(WS_ID, taskId) === ["tasks","runs",id]
      if (opts.queryKey?.[0] === "agent-runs" && opts.queryKey?.[2] === "task") {
        return { data: mockState.runs };
      }
      return { data: undefined };
    },
  };
});

import { TaskAgentHeaderChip } from "./task-agent-header-chip";

function makeRun(overrides: Partial<AgentRun>): AgentRun {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    task_id: "task-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.runs = [];
  mockState.triggerProps = undefined;
});

describe("TaskAgentHeaderChip", () => {
  it("shows the active agent name without event count or elapsed time", () => {
    mockState.runs = [makeRun({})];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />);

    expect(
      screen.getByRole("button", { name: "Walt is working" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Walt is working")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/\d+[smh]/i)).not.toBeInTheDocument();
    expect(mockState.agentRunMessagesOptions).not.toHaveBeenCalled();
  });

  it("keeps the header popover card with active task rows", () => {
    mockState.runs = [makeRun({ id: "task-running" })];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />);

    expect(screen.getByTestId("agent-popover-content")).toBeInTheDocument();
    expect(screen.getByTestId("active-task-row")).toHaveTextContent(
      "task-running",
    );
    expect(mockState.agentRunMessagesOptions).not.toHaveBeenCalled();
  });

  it("opens the activity card on hover, not only on click", () => {
    mockState.runs = [makeRun({})];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />);

    // Base UI gates hover-to-open on `openOnHover` on the trigger. Without it
    // the chip would be click-only, which is the behavior SILI-3507 replaces.
    // The trigger stays a real <button>, so click/keyboard access is retained.
    expect(mockState.triggerProps?.openOnHover).toBe(true);
    expect(
      screen.getByRole("button", { name: "Walt is working" }),
    ).toBeInTheDocument();
  });

  it("uses the concise multi-agent working label", () => {
    mockState.runs = [
      makeRun({ id: "task-1", agent_id: "agent-1" }),
      makeRun({ id: "task-2", agent_id: "agent-2" }),
    ];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />);

    expect(
      screen.getByRole("button", { name: "2 agents working" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("2 agents working")).toHaveLength(2);
    expect(screen.getAllByTestId("active-task-row")).toHaveLength(2);
    expect(mockState.agentRunMessagesOptions).not.toHaveBeenCalled();
  });

  it("uses the requested Chinese single-agent copy", () => {
    mockState.runs = [makeRun({})];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />, {
      locale: "zh-Hans",
    });

    expect(screen.getByText("Walt 在工作")).toBeInTheDocument();
  });

  it("does not render when the task has only terminal tasks", () => {
    // The list is task-scoped by the endpoint, so the chip's only job is to
    // ignore terminal statuses (those are the execution log's story).
    mockState.runs = [
      makeRun({
        id: "task-done",
        status: "completed",
        completed_at: "2026-06-08T08:05:00Z",
      }),
      makeRun({
        id: "task-cancelled",
        status: "cancelled",
        completed_at: "2026-06-08T08:06:00Z",
      }),
    ];

    renderWithI18n(<TaskAgentHeaderChip taskId="task-1" />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
