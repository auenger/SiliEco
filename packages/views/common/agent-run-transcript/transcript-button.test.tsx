// @vitest-environment jsdom

import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  type RenderResult,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { api } from "@silieco/core/api";
import {
  chatKeys,
  mergeAgentRunMessagesBySeq,
} from "@silieco/core/chat/queries";
import type { AgentRun } from "@silieco/core/types/agent";
import type { AgentRunMessagePayload } from "@silieco/core/types/events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TranscriptButton } from "./transcript-button";
import type { TimelineItem } from "./build-timeline";

vi.mock("@silieco/core/api", () => ({
  api: {
    listAgentRunMessages: vi.fn(),
  },
}));

// Render the timeline items so tests can assert the dialog grows in place.
// `tool_use` / `tool_result` entries don't coalesce, so each message stays a
// distinct row — unlike adjacent text/thinking, which buildTimeline merges.
vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({
    open,
    onOpenChange,
    items,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    items: TimelineItem[];
  }) =>
    open ? (
      <div role="dialog" data-testid="transcript-dialog">
        <button type="button" onClick={() => onOpenChange(false)}>
          Close
        </button>
        {items.map((item) => (
          <div key={item.seq} data-testid="event" data-seq={item.seq} />
        ))}
      </div>
    ) : null,
}));

const LIVE_RUN_ID = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

const baseTask: AgentRun = {
  id: LIVE_RUN_ID,
  agent_id: "agent-1",
  runtime_id: "",
  task_id: "task-1",
  status: "running",
  priority: 0,
  dispatched_at: "2026-05-15T10:00:05.000Z",
  started_at: "2026-05-15T10:00:06.000Z",
  completed_at: null,
  result: null,
  error: null,
  created_at: "2026-05-15T10:00:00.000Z",
};

const msg = (seq: number, tool: string): AgentRunMessagePayload => ({
  run_id: LIVE_RUN_ID,
  task_id: "task-1",
  seq,
  type: "tool_use",
  tool,
  input: { i: String(seq) },
});

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderWith(qc: QueryClient, ui: React.ReactNode): RenderResult {
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const listAgentRunMessages = vi.mocked(api.listAgentRunMessages);

beforeEach(() => {
  listAgentRunMessages.mockReset();
  listAgentRunMessages.mockResolvedValue([]);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("TranscriptButton", () => {
  it("closes the transcript dialog when desktop navigation starts", async () => {
    const items: TimelineItem[] = [{ seq: 1, type: "text", content: "hello" }];
    const qc = newClient();
    renderWith(
      qc,
      <TranscriptButton
        run={{ ...baseTask, status: "completed" }}
        agentName="Codex"
        items={items}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(
        new CustomEvent("silieco:navigate", {
          detail: { path: "/acme/inbox?task=SILI-123" },
        }),
      );
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("live mode: the open dialog grows as the shared cache receives new messages", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.agentRunMessages(LIVE_RUN_ID), [msg(1, "Bash")]);
    listAgentRunMessages.mockResolvedValue([msg(1, "Bash")]);

    renderWith(qc, <TranscriptButton run={baseTask} agentName="Codex" isLive />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );

    // Simulate a WS `run:message` append into the shared cache.
    act(() => {
      qc.setQueryData<AgentRunMessagePayload[]>(
        chatKeys.agentRunMessages(LIVE_RUN_ID),
        (old = []) => mergeAgentRunMessagesBySeq(old, [msg(2, "Read")]),
      );
    });

    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(2),
    );
  });

  it("live mode: forces a backfill on open even when the cache already has data", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.agentRunMessages(LIVE_RUN_ID), [
      msg(1, "Bash"),
      msg(2, "Read"),
    ]);
    listAgentRunMessages.mockResolvedValue([msg(1, "Bash"), msg(2, "Read")]);

    renderWith(qc, <TranscriptButton run={baseTask} agentName="Codex" isLive />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    await waitFor(() =>
      expect(listAgentRunMessages).toHaveBeenCalledWith(LIVE_RUN_ID),
    );
  });

  it("terminal mode: fetches once on open and does not subscribe to the cache", async () => {
    const qc = newClient();
    listAgentRunMessages.mockResolvedValue([msg(1, "Bash")]);

    renderWith(
      qc,
      <TranscriptButton
        run={{ ...baseTask, status: "completed", completed_at: "2026-05-15T10:00:10.000Z" }}
        agentName="Codex"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );

    // A later cache write must NOT reach the terminal dialog: it renders a
    // one-shot local snapshot, never an observer of the shared cache.
    act(() => {
      qc.setQueryData(chatKeys.agentRunMessages(LIVE_RUN_ID), [
        msg(1, "Bash"),
        msg(2, "Read"),
      ]);
    });

    expect(screen.getAllByTestId("event")).toHaveLength(1);
    expect(listAgentRunMessages).toHaveBeenCalledTimes(1);
  });

  it("running→terminal: keeps the dialog populated and takes a final backfill", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.agentRunMessages(LIVE_RUN_ID), [msg(1, "Bash")]);
    listAgentRunMessages.mockResolvedValue([msg(1, "Bash")]);

    const { rerender } = renderWith(
      qc,
      <TranscriptButton run={baseTask} agentName="Codex" isLive />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );
    await waitFor(() => expect(listAgentRunMessages).toHaveBeenCalledTimes(1));

    // Task finishes: parent flips isLive→false and the status to terminal.
    rerender(
      <QueryClientProvider client={qc}>
        <TranscriptButton
          run={{ ...baseTask, status: "completed", completed_at: "2026-05-15T10:00:10.000Z" }}
          agentName="Codex"
          isLive={false}
        />
      </QueryClientProvider>,
    );

    // Dialog stays mounted (latched), and the terminal transition triggers a
    // second authoritative backfill rather than blanking to local state.
    expect(screen.getByTestId("transcript-dialog")).toBeInTheDocument();
    await waitFor(() => expect(listAgentRunMessages).toHaveBeenCalledTimes(2));

    // The final tail message still flows in through the shared cache.
    act(() => {
      qc.setQueryData<AgentRunMessagePayload[]>(
        chatKeys.agentRunMessages(LIVE_RUN_ID),
        (old = []) => mergeAgentRunMessagesBySeq(old, [msg(2, "Read")]),
      );
    });
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(2),
    );
  });
});
