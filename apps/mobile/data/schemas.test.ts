import { describe, expect, it } from "vitest";
import { TaskSchema } from "@silieco/core/api/schemas";
import {
  ActiveRunsResponseSchema,
  AgentRunMessagePayloadSchema,
  AgentRunSchema,
  ChatPendingRunSchema,
  EMPTY_TASK_FALLBACK,
} from "./schemas";

describe("Task / AgentRun contract", () => {
  it("keeps business task and execution identities separate", () => {
    const message = AgentRunMessagePayloadSchema.parse({
      run_id: "run-1",
      task_id: "task-1",
      seq: 1,
      type: "text",
      content: "Working",
    });
    expect(message.run_id).toBe("run-1");
    expect(message.task_id).toBe("task-1");

    const task = TaskSchema.parse({
      ...EMPTY_TASK_FALLBACK,
      id: "task-1",
      parent_task_id: "parent-task",
    });
    expect(task.parent_task_id).toBe("parent-task");

    const run = AgentRunSchema.parse({
      id: "run-1",
      task_id: task.id,
      parent_run_id: "parent-run",
      status: "running",
    });
    expect(run.task_id).toBe(task.id);
    expect(run.parent_run_id).toBe("parent-run");
  });

  it("retains runs from the new response envelope", () => {
    const response = ActiveRunsResponseSchema.parse({
      runs: [{ id: "run-1", task_id: "task-1", status: "running" }],
    });
    expect(response.runs).toHaveLength(1);
    expect(response.runs[0].id).toBe("run-1");
  });

  it("retains the active local-directory waiting state", () => {
    const response = ActiveRunsResponseSchema.parse({
      runs: [{ id: "run-1", task_id: "task-1", status: "waiting_local_directory" }],
    });
    expect(response.runs[0].status).toBe("waiting_local_directory");
  });

  it("allows chat execution without a business task", () => {
    const run = AgentRunSchema.parse({
      id: "chat-run",
      status: "queued",
      chat_session_id: "chat-1",
    });
    expect(run.task_id).toBe("");
    expect(ChatPendingRunSchema.parse({ run_id: run.id }).run_id).toBe(run.id);
  });

  it("rejects a malformed execution identity", () => {
    expect(
      AgentRunMessagePayloadSchema.safeParse({
        run_id: 123,
        task_id: "task-1",
        seq: 1,
        type: "text",
      }).success,
    ).toBe(false);
  });
});
