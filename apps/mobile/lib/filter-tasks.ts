/**
 * Mirrors the status+priority slice of `filterTasks()` at
 * packages/views/tasks/utils/filter.ts:30-34. Same predicate, same
 * "empty array = show all" semantics — required by the same-N parity rule
 * in apps/mobile/CLAUDE.md.
 *
 * Mobile only filters on status + priority for now; assignee / project /
 * label slots from the web filter are deferred to v2.
 */
import type { Task, TaskPriority, TaskStatus } from "@silieco/core/types";

export function filterTasks(
  tasks: Task[],
  statusFilters: TaskStatus[],
  priorityFilters: TaskPriority[],
): Task[] {
  return tasks.filter((task) => {
    if (
      statusFilters.length > 0 &&
      !statusFilters.includes(task.status)
    ) {
      return false;
    }
    if (
      priorityFilters.length > 0 &&
      !priorityFilters.includes(task.priority)
    ) {
      return false;
    }
    return true;
  });
}
