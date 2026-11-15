import type { Task, TaskStatus } from "@silieco/core/types";

export const WORKFLOW_TASK_STATUSES: TaskStatus[] = [
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "backlog",
  "done",
  "cancelled",
];

export function sortWorkflowTasksByLifecycle(tasks: Task[]): Task[] {
  const rank = new Map(
    WORKFLOW_TASK_STATUSES.map((status, index) => [status, index]),
  );
  return [...tasks].sort(
    (left, right) =>
      (rank.get(left.status) ?? WORKFLOW_TASK_STATUSES.length) -
        (rank.get(right.status) ?? WORKFLOW_TASK_STATUSES.length) ||
      left.position - right.position ||
      left.created_at.localeCompare(right.created_at),
  );
}
