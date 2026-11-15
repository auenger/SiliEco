// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentRun, Task } from "@silieco/core/types";
import { renderWithI18n } from "../../test/i18n";

// The hover card renders one row per task and counts tasks, so its header
// must describe tasks — not agents. A single agent can run several tasks at
// once (e.g. the workspace chip reads "2 working" for two unique agents while
// the card lists three task rows). An agent-worded header here would print
// "3 agents working" for those two agents, contradicting the chip. SILI-3872.

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@silieco/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ "agent-1": "Niko", "agent-2": "J" })[id] ?? "Unknown Agent",
    getActorInitials: (_type: string, id: string) =>
      ({ "agent-1": "NI", "agent-2": "J" })[id] ?? "UA",
    getActorAvatarUrl: () => null,
  }),
}));

// The card only reads these query results for avatars / availability, never
// for the header count, so empty lists keep the row chrome inert while the
// header still derives from the task array.
vi.mock("@silieco/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@silieco/core/agents", () => ({
  deriveAgentAvailability: () => "online",
}));

vi.mock("@silieco/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="actor-avatar">{name}</span>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return { ...actual, useQuery: () => ({ data: [] }) };
});

import {
  AgentActivityHoverContent,
  WorkspaceAgentActivityHoverContent,
} from "./agent-activity-hover-content";

function makeTask(id: string, identifier: string, title: string): Task {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier,
    title,
    description: null,
    status: "in_progress",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-06-08T08:00:00Z",
    updated_at: "2026-06-08T08:00:00Z",
  };
}

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

afterEach(cleanup);

describe("AgentActivityHoverContent", () => {
  // Two agents, three running tasks (Niko runs two at once). The header must
  // count the three task rows, not the two agents.
  const threeTasksTwoAgents = [
    makeRun({ id: "t1", agent_id: "agent-1" }),
    makeRun({ id: "t2", agent_id: "agent-1" }),
    makeRun({ id: "t3", agent_id: "agent-2" }),
  ];

  it("counts runs, not agents, in the header", () => {
    renderWithI18n(<AgentActivityHoverContent runs={threeTasksTwoAgents} />);

    expect(screen.getByText("3 runs working")).toBeInTheDocument();
    // The old agent-worded copy would have read "3 agents working" here and
    // disagreed with the chip's unique-agent count.
    expect(screen.queryByText(/agents? working/)).not.toBeInTheDocument();
    // One row per task — three avatars for three tasks.
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(3);
  });

  it("uses the singular task copy for a single task", () => {
    renderWithI18n(<AgentActivityHoverContent runs={[makeRun({})]} />);

    expect(screen.getByText("1 run working")).toBeInTheDocument();
  });

  it("renders the requested Chinese task copy", () => {
    renderWithI18n(<AgentActivityHoverContent runs={threeTasksTwoAgents} />, {
      locale: "zh-Hans",
    });

    expect(screen.getByText("3 个 run 工作中")).toBeInTheDocument();
  });
});

// The workspace chip says WHO is working ("N agents working"). This card
// says WHERE: the two figures the chip does not carry, and the rows grouped
// by task. It stays silent about work it excludes — see the component doc.
// SILI-4884.
describe("WorkspaceAgentActivityHoverContent", () => {
  it("carries the two figures the chip does not, and groups rows by task", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        tasks={[
          makeTask("i-1", "SILI-4879", "Counting logic looks wrong"),
          makeTask("i-2", "SILI-4881", "daemon extra work dir"),
          makeTask("i-3", "SILI-4883", "First PR review flow"),
        ]}
        runsByTaskId={
          new Map([
            [
              "i-1",
              [
                makeRun({ id: "t1", agent_id: "agent-1", task_id: "i-1" }),
                makeRun({ id: "t4", agent_id: "agent-2", task_id: "i-1" }),
              ],
            ],
            ["i-2", [makeRun({ id: "t2", agent_id: "agent-1", task_id: "i-2" })]],
            ["i-3", [makeRun({ id: "t3", agent_id: "agent-2", task_id: "i-3" })]],
          ])
        }
        runCount={4}
        />,
    );

    expect(screen.getByText("3 tasks · 4 runs")).toBeInTheDocument();
    // Rows group under their task, mirroring what clicking the chip does.
    expect(screen.getByText("SILI-4879")).toBeInTheDocument();
    expect(screen.getByText("Counting logic looks wrong")).toBeInTheDocument();
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(4);
  });

  it("says nothing about work it excludes", () => {
    // Chat/autopilot runs and out-of-scope tasks leave no trace on this page,
    // so a "not counted" footnote would explain an absence the user never
    // perceived. The card only ever describes what IS counted.
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        tasks={[makeTask("i-1", "SILI-1", "One")]}
        runsByTaskId={
          new Map([["i-1", [makeRun({ id: "t1", task_id: "i-1" })]]])
        }
        runCount={1}
      />,
    );

    expect(screen.queryByText(/not counted/)).not.toBeInTheDocument();
    expect(screen.getByText("1 task · 1 run")).toBeInTheDocument();
  });

  it("falls back to the agent-worded empty copy when nothing is counted", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        tasks={[]}
        runsByTaskId={new Map()}
        runCount={0}
      />,
    );

    expect(screen.getByText("No agents working right now")).toBeInTheDocument();
    expect(screen.queryByText(/not counted/)).not.toBeInTheDocument();
  });

  it("renders the Chinese copy for the counted units", () => {
    renderWithI18n(
      <WorkspaceAgentActivityHoverContent
        tasks={[makeTask("i-1", "SILI-1", "One")]}
        runsByTaskId={
          new Map([["i-1", [makeRun({ id: "t1", task_id: "i-1" })]]])
        }
        runCount={2}
      />,
      { locale: "zh-Hans" },
    );

    // task / task stay lowercase English in UI strings (conventions.zh.mdx).
    expect(screen.getByText("1 个 task · 2 个 run")).toBeInTheDocument();
  });
});
