import { describe, expect, it } from "vitest";
import type { Task, TaskStatus } from "@silieco/core/types";
import { sortWorkflowTasksByLifecycle } from "./workflow-task-sort";

function task(
  id: string,
  status: TaskStatus,
  position: number,
): Task {
  return {
    id,
    workspace_id: "workspace-1",
    number: Number(id.replace(/\D/g, "")),
    identifier: id.toUpperCase(),
    title: id,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_task_id: null,
    project_id: "project-1",
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("sortWorkflowTasksByLifecycle", () => {
  it("puts Todo first and keeps position order inside each lifecycle status", () => {
    const sorted = sortWorkflowTasksByLifecycle([
      task("done-1", "done", 0),
      task("progress-1", "in_progress", 0),
      task("todo-2", "todo", 20),
      task("todo-1", "todo", 10),
      task("backlog-1", "backlog", 0),
    ]);

    expect(sorted.map((item) => item.id)).toEqual([
      "todo-1",
      "todo-2",
      "progress-1",
      "backlog-1",
      "done-1",
    ]);
  });
});
