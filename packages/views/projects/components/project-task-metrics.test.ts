import { describe, expect, it } from "vitest";
import { getProjectTaskMetrics } from "./project-task-metrics";

describe("getProjectTaskMetrics", () => {
  it("surfaces project-level totals from the project record", () => {
    const metrics = getProjectTaskMetrics({ task_count: 9, done_count: 5 });

    expect(metrics).toEqual({
      totalCount: 9,
      completedCount: 5,
    });
  });
});
