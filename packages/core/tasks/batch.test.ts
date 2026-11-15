import { describe, it, expect } from "vitest";
import type { Task } from "../types";
import { commonTaskFields } from "./batch";

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "task-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "SILI-1",
    title: "Task 1",
    description: null,
    status: "todo",
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
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("commonTaskFields", () => {
  it("returns all-null for an empty selection (nothing to reflect)", () => {
    expect(commonTaskFields([])).toEqual({
      status: null,
      priority: null,
      assignee: null,
    });
  });

  it("reflects a single task's own fields", () => {
    const common = commonTaskFields([
      makeTask({ status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ]);
    expect(common.status).toBe("in_progress");
    expect(common.priority).toBe("high");
    expect(common.assignee).toEqual({ type: "member", id: "u-1" });
  });

  it("returns the shared status when every task agrees, not a hardcoded default", () => {
    // Regression for SILI-3510: the batch picker used to assert "todo"
    // regardless of the selection.
    const common = commonTaskFields([
      makeTask({ id: "a", status: "in_review" }),
      makeTask({ id: "b", status: "in_review" }),
    ]);
    expect(common.status).toBe("in_review");
  });

  it("returns null status when the selection spans different statuses (mixed)", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", status: "todo" }),
      makeTask({ id: "b", status: "done" }),
    ]);
    expect(common.status).toBeNull();
  });

  it("derives each field independently — shared status, mixed priority", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", status: "blocked", priority: "urgent" }),
      makeTask({ id: "b", status: "blocked", priority: "low" }),
    ]);
    expect(common.status).toBe("blocked");
    expect(common.priority).toBeNull();
  });

  it("treats an all-unassigned selection as a real shared value, not mixed", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", assignee_type: null, assignee_id: null }),
      makeTask({ id: "b", assignee_type: null, assignee_id: null }),
    ]);
    expect(common.assignee).toEqual({ type: null, id: null });
  });

  it("returns the shared assignee when every task points at the same actor", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", assignee_type: "agent", assignee_id: "agent-1" }),
      makeTask({ id: "b", assignee_type: "agent", assignee_id: "agent-1" }),
    ]);
    expect(common.assignee).toEqual({ type: "agent", id: "agent-1" });
  });

  it("returns null assignee when actors differ", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", assignee_type: "member", assignee_id: "u-1" }),
      makeTask({ id: "b", assignee_type: "member", assignee_id: "u-2" }),
    ]);
    expect(common.assignee).toBeNull();
  });

  it("returns null assignee when some are assigned and some are unassigned", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", assignee_type: "member", assignee_id: "u-1" }),
      makeTask({ id: "b", assignee_type: null, assignee_id: null }),
    ]);
    expect(common.assignee).toBeNull();
  });

  it("distinguishes assignees of the same id but different type", () => {
    const common = commonTaskFields([
      makeTask({ id: "a", assignee_type: "member", assignee_id: "x" }),
      makeTask({ id: "b", assignee_type: "agent", assignee_id: "x" }),
    ]);
    expect(common.assignee).toBeNull();
  });
});
