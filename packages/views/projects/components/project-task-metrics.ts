import type { Project } from "@silieco/core/types";

export function getProjectTaskMetrics(
  project: Pick<Project, "task_count" | "done_count">,
) {
  return {
    totalCount: project.task_count,
    completedCount: project.done_count,
  };
}
