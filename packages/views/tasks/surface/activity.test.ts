import { describe, expect, it } from "vitest";
import type { AgentRun } from "@silieco/core/types";
import { deriveTaskSurfaceActivity, selectTaskRuns } from "./activity";

function task(overrides: Partial<AgentRun>): AgentRun {
  return {
    id: overrides.id ?? "task-1",
    workspace_id: "ws-1",
    task_id: undefined,
    agent_id: "agent-1",
    status: "queued",
    trigger_type: "manual",
    trigger_source: "manual",
    trigger_ref: null,
    prompt: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    started_at: null,
    completed_at: null,
    ...overrides,
  } as AgentRun;
}

describe("deriveTaskSurfaceActivity", () => {
  it("separates running work from queued-only task activity", () => {
    const activity = deriveTaskSurfaceActivity([
      task({ id: "run-1", task_id: "i-1", status: "running" }),
      task({ id: "queue-1", task_id: "i-2", status: "queued" }),
      task({ id: "dispatch-1", task_id: "i-2", status: "dispatched" }),
      task({ id: "wait-1", task_id: "i-3", status: "waiting_local_directory" }),
      task({ id: "done-1", task_id: "i-4", status: "completed" }),
      task({ id: "no-task", task_id: undefined, status: "running" }),
    ]);

    expect(activity.runningTaskIds).toEqual(new Set(["i-1"]));
    expect(activity.activityByTaskId.get("i-1")).toMatchObject({
      isWorking: true,
      isQueued: false,
    });
    expect(activity.activityByTaskId.get("i-2")).toMatchObject({
      isWorking: false,
      isQueued: true,
    });
    expect(activity.activityByTaskId.get("i-3")).toMatchObject({
      isWorking: false,
      isQueued: true,
    });
    expect(activity.activityByTaskId.has("i-4")).toBe(false);
  });
});

describe("selectTaskRuns", () => {
  const snapshot: AgentRun[] = [
    task({ id: "run-1", task_id: "i-1", status: "running" }),
    task({ id: "run-2", task_id: "i-1", status: "running" }),
    task({ id: "queue-1", task_id: "i-1", status: "queued" }),
    task({ id: "other-run", task_id: "i-2", status: "running" }),
    task({ id: "wait-1", task_id: "i-3", status: "waiting_local_directory" }),
    task({ id: "dispatch-1", task_id: "i-3", status: "dispatched" }),
    task({ id: "done-1", task_id: "i-1", status: "completed" }),
    task({ id: "no-task", task_id: undefined, status: "running" }),
  ];

  it("returns only the running/queued tasks for the requested task", () => {
    const groups = selectTaskRuns(snapshot, "i-1");
    expect(groups.running.map((t) => t.id)).toEqual(["run-1", "run-2"]);
    expect(groups.queued.map((t) => t.id)).toEqual(["queue-1"]);
  });

  it("treats dispatched and waiting_local_directory as queued", () => {
    const groups = selectTaskRuns(snapshot, "i-3");
    expect(groups.running).toEqual([]);
    expect(groups.queued.map((t) => t.id)).toEqual(["wait-1", "dispatch-1"]);
  });

  it("drops terminal statuses and tasks without an task", () => {
    const groups = selectTaskRuns(snapshot, "i-1");
    expect(groups.running.map((t) => t.id)).not.toContain("done-1");
    expect(groups.queued.map((t) => t.id)).not.toContain("done-1");
    const noMatch = selectTaskRuns(snapshot, "does-not-exist");
    expect(noMatch).toEqual({ running: [], queued: [] });
  });
});
