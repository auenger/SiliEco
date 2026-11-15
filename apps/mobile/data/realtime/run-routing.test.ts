import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, useQueryClient } from "@tanstack/react-query";
import type { AgentRunMessagePayload, WSEventPayload, WSEventType } from "@silieco/core/types";
import type { WSClient } from "./ws-client";

import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import { chatKeys } from "@/data/queries/chat";
import { taskKeys } from "@/data/queries/task-keys";
import { agentRunsKeys } from "@/data/queries/agent-runs";
import { useChatSessionRealtime as registerChatSessionSubscriptions } from "./use-chat-session-realtime";
import { useTaskRealtime as registerTaskSubscriptions } from "./use-task-realtime";

vi.mock("@/data/api", () => ({ api: {} }));
vi.mock("@/lib/use-ws-subscriptions", () => ({ useWSSubscriptions: vi.fn() }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...await importOriginal<typeof import("@tanstack/react-query")>(),
  useQueryClient: vi.fn(),
}));



let qc: QueryClient;
const handlers = new Map<WSEventType, (payload: unknown) => void>();

beforeEach(() => {
  qc = new QueryClient();
  handlers.clear();
  vi.mocked(useQueryClient).mockReturnValue(qc);
  vi.mocked(useWSSubscriptions).mockImplementation((setup) => {
    setup({
      on: (event: WSEventType, handler: (payload: unknown) => void) => {
        handlers.set(event, handler);
        return () => handlers.delete(event);
      },
      onReconnect: () => () => {},
    } as unknown as WSClient, "workspace-1");
  });
});

function emit<E extends WSEventType>(event: E, payload: WSEventPayload<E>) {
  handlers.get(event)?.(payload);
}

describe("execution realtime routing", () => {
  it("routes chat run messages by session and stores the execution identity", () => {
    registerChatSessionSubscriptions("chat-1");
    emit("run:message", {
      run_id: "run-1", task_id: "", chat_session_id: "chat-1",
      seq: 1, type: "text", content: "Working",
    });
    emit("run:message", {
      run_id: "run-2", task_id: "", chat_session_id: "chat-2",
      seq: 1, type: "text", content: "Other chat",
    });
    emit("run:message", {
      run_id: "run-3", task_id: "task-1",
      seq: 1, type: "text", content: "Business task run",
    });

    expect(qc.getQueryData<AgentRunMessagePayload[]>(chatKeys.runMessages("run-1")))
      .toEqual([expect.objectContaining({ run_id: "run-1", task_id: "", chat_session_id: "chat-1" })]);
    expect(qc.getQueryData(chatKeys.runMessages("run-2"))).toBeUndefined();
    expect(qc.getQueryData(chatKeys.runMessages("run-3"))).toBeUndefined();
  });

  it("scopes run progress to the business task rather than the execution id", () => {
    registerTaskSubscriptions("task-1");
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    emit("run:progress", { run_id: "task-1", task_id: "task-2", summary: "Other task" });
    emit("run:progress", { run_id: "task-1", chat_session_id: "chat-1", summary: "Chat only" });
    expect(invalidate).not.toHaveBeenCalled();

    emit("run:progress", { run_id: "run-1", task_id: "task-1", summary: "Task progress" });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: taskKeys.detail("workspace-1", "task-1") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: agentRunsKeys.activeForTask("workspace-1", "task-1") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: agentRunsKeys.historyForTask("workspace-1", "task-1") });
  });
});
