import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { PROTOCOL_GENERATION, PROTOCOL_HEADER } from "../constants/protocol";

afterEach(() => vi.unstubAllGlobals());

function response(body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
  }));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("Task and AgentRun contract", () => {
  it("keeps business identity separate from execution identity in transcript messages", async () => {
    const fetchMock = response([{
      run_id: "execution-1", task_id: "business-1", seq: 7, type: "text", content: "Done",
    }]);
    const messages = await new ApiClient("https://api.example.test").listAgentRunMessages("execution-1");
    expect(messages).toMatchObject([{ run_id: "execution-1", task_id: "business-1", seq: 7 }]);
    expect(fetchMock).toHaveBeenCalledWith("https://api.example.test/api/runs/execution-1/messages", expect.objectContaining({
      headers: expect.objectContaining({ [PROTOCOL_HEADER]: PROTOCOL_GENERATION }),
    }));
  });

  it("does not reinterpret a business id as a missing execution id", async () => {
    response([{ task_id: "business-1", seq: 7, type: "text" }]);
    await expect(new ApiClient("https://api.example.test").listAgentRunMessages("execution-1")).resolves.toEqual([]);
  });

  it("allows chat execution messages without a linked business task", async () => {
    response([{ run_id: "execution-1", chat_session_id: "chat-1", seq: 1, type: "text" }]);
    await expect(new ApiClient("https://api.example.test").listAgentRunMessages("execution-1"))
      .resolves.toMatchObject([{ run_id: "execution-1", task_id: "", chat_session_id: "chat-1" }]);
  });

  it("reads the run collection under the business task endpoint", async () => {
    const fetchMock = response({ runs: [{ id: "execution-1", task_id: "business-1", status: "running" }] });
    await expect(new ApiClient("https://api.example.test").getActiveRunsForTask("business-1"))
      .resolves.toMatchObject({ runs: [{ id: "execution-1", task_id: "business-1" }] });
    expect(fetchMock.mock.calls[0]?.[0]).toBe("https://api.example.test/api/tasks/business-1/active-run");
  });

  it("rejects a malformed business task instead of returning an execution-shaped object", async () => {
    response({ id: "execution-1", task_id: "business-1", status: "running" });
    await expect(new ApiClient("https://api.example.test").getTask("business-1")).rejects.toThrow("Invalid task response");
  });
  it("validates run counts and rejects malformed usage metrics", async () => {
    response({ run_count: 3, total_input_tokens: 7 });
    await expect(new ApiClient("https://api.example.test").getTaskUsage("business-1"))
      .resolves.toMatchObject({ run_count: 3, total_input_tokens: 7 });
    response({ run_count: "bad" });
    await expect(new ApiClient("https://api.example.test").getTaskUsage("business-1"))
      .resolves.toMatchObject({ run_count: 0, total_input_tokens: 0 });
  });

  it("rejects malformed cancellation and retry responses", async () => {
    response({ status: "cancelled" });
    await expect(new ApiClient("https://api.example.test").cancelRun("business-1", "execution-1"))
      .rejects.toThrow("Invalid agent run response");
    response({ status: "queued" });
    await expect(new ApiClient("https://api.example.test").rerunTask("business-1", "execution-1"))
      .rejects.toThrow("Invalid agent run response");
  });

});
