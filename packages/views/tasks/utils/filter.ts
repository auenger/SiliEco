import type { Task, TaskStatus, TaskPriority, TaskAssigneeGroup } from "@silieco/core/types";
import type { ActorFilterValue } from "@silieco/core/tasks/stores/view-store";
import type { TaskActivityState } from "../surface/activity";

export interface TaskFilters {
  statusFilters: TaskStatus[];
  priorityFilters: TaskPriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** Keeps an explicitly active assignee predicate distinct from the normal
   *  empty-array = no-filter state. When true with no selected assignees and
   *  includeNoAssignee=false, the predicate intentionally matches nothing. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  /** Custom-property filters: definition id → selected option ids (OR within
   *  a definition, AND across definitions; checkbox uses "true"/"false"). */
  propertyFilters?: Record<string, string[]>;
  // When `agentRunningFilter` is true, only keep tasks whose id is in
  // `runningTaskIds`. The surface derives this set from the independent
  // `/api/working-agents` projection so filter.ts stays free of fetching.
  agentRunningFilter?: boolean;
  runningTaskIds?: ReadonlySet<string>;
  // "Show sub-tasks" display toggle. When explicitly `false`, hide tasks
  // that have a parent so only top-level tasks remain. Undefined / true keeps
  // the default behaviour of showing everything, so existing callers that omit
  // it (and mobile's positional variant) are unaffected.
  showSubTasks?: boolean;
}

export interface TaskFilterState {
  statusFilters: TaskStatus[];
  priorityFilters: TaskPriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** See TaskFilters.assigneeFilterActive. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters?: Record<string, string[]>;
  workingOnly: boolean;
  /** See TaskFilters.showSubTasks — only an explicit `false` hides. */
  showSubTasks?: boolean;
}

export interface TaskFilterContext {
  activityByTaskId?: ReadonlyMap<string, TaskActivityState>;
  runningTaskIds?: ReadonlySet<string>;
}

/**
 * Match one task against the property filters. Select values are single
 * option-id strings, multi_select values are option-id arrays, checkbox
 * values are booleans compared against the "true"/"false" pseudo-options.
 * An task with no value for a filtered definition never matches it.
 */
export function taskMatchesPropertyFilters(
  task: Task,
  propertyFilters: Record<string, string[]> | undefined,
): boolean {
  if (!propertyFilters) return true;
  for (const [propertyId, selected] of Object.entries(propertyFilters)) {
    if (selected.length === 0) continue;
    const value = task.properties?.[propertyId];
    if (value === undefined) return false;
    if (typeof value === "string") {
      if (!selected.includes(value)) return false;
    } else if (Array.isArray(value)) {
      if (!value.some((id) => selected.includes(id))) return false;
    } else if (typeof value === "boolean") {
      if (!selected.includes(String(value))) return false;
    } else {
      return false;
    }
  }
  return true;
}

function taskIsWorking(taskId: string, context: TaskFilterContext) {
  if (context.activityByTaskId) {
    return context.activityByTaskId.get(taskId)?.isWorking === true;
  }
  return context.runningTaskIds?.has(taskId) === true;
}

/**
 * Filter tasks using positive selection model.
 * Empty arrays = no filter (show all). Non-empty = show only matching.
 *
 * Assignee has a special "No assignee" toggle (includeNoAssignee):
 * - When only includeNoAssignee is true → show only unassigned tasks
 * - When assigneeFilters has items → show only those assignees' tasks
 * - When both → show matching assignees + unassigned
 */
export function applyTaskFilters(
  tasks: Task[],
  filters: TaskFilterState,
  context: TaskFilterContext = {},
): Task[] {
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, workingOnly } = filters;
  const hasAssigneeFilter =
    filters.assigneeFilterActive === true ||
    assigneeFilters.length > 0 ||
    includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  // Empty set passed without `agentRunningFilter` is a no-op. When the
  // filter is on but the set is missing/empty, hide everything — the
  // user opted into "only running" and there is nothing running.
  const applyWorkingOnly = workingOnly === true;
  const hideSubTasks = filters.showSubTasks === false;

  return tasks.filter((task) => {
    if (applyWorkingOnly && !taskIsWorking(task.id, context))
      return false;

    if (hideSubTasks && task.parent_task_id) return false;

    if (statusFilters.length > 0 && !statusFilters.includes(task.status))
      return false;

    if (priorityFilters.length > 0 && !priorityFilters.includes(task.priority))
      return false;

    if (hasAssigneeFilter) {
      if (!task.assignee_id) {
        // Unassigned task — show only if "No assignee" is checked
        if (!includeNoAssignee) return false;
      } else if (assigneeFilters.length > 0) {
        // Assigned task — show only if assignee is in the filter list
        if (!assigneeFilters.some(
          (f) => f.type === task.assignee_type && f.id === task.assignee_id,
        )) return false;
      } else {
        // Only "No assignee" is checked, no specific assignees → hide assigned tasks
        return false;
      }
    }

    if (
      creatorFilters.length > 0 &&
      !creatorFilters.some(
        (f) => f.type === task.creator_type && f.id === task.creator_id,
      )
    ) {
      return false;
    }

    if (hasProjectFilter) {
      if (!task.project_id) {
        if (!includeNoProject) return false;
      } else if (projectFilters.length > 0) {
        if (!projectFilters.includes(task.project_id)) return false;
      } else {
        // Only "No project" is checked → hide tasks that have a project
        return false;
      }
    }

    if (labelFilters.length > 0) {
      // OR semantics within the filter: keep tasks that carry any of the
      // selected labels. Matches existing priority / project multi-select.
      const taskLabels = task.labels;
      if (!taskLabels || taskLabels.length === 0) return false;
      if (!taskLabels.some((l) => labelFilters.includes(l.id))) return false;
    }

    if (!taskMatchesPropertyFilters(task, filters.propertyFilters)) return false;

    return true;
  });
}

export function filterTasks(tasks: Task[], filters: TaskFilters): Task[] {
  return applyTaskFilters(
    tasks,
    {
      statusFilters: filters.statusFilters,
      priorityFilters: filters.priorityFilters,
      assigneeFilters: filters.assigneeFilters,
      includeNoAssignee: filters.includeNoAssignee,
      assigneeFilterActive: filters.assigneeFilterActive,
      creatorFilters: filters.creatorFilters,
      projectFilters: filters.projectFilters,
      includeNoProject: filters.includeNoProject,
      labelFilters: filters.labelFilters,
      propertyFilters: filters.propertyFilters,
      workingOnly: filters.agentRunningFilter === true,
      showSubTasks: filters.showSubTasks,
    },
    { runningTaskIds: filters.runningTaskIds },
  );
}

/**
 * Re-apply the client-only display filters to a server-grouped response.
 * The assignee-grouped board renders straight from `groups`, bypassing the
 * flat `applyTaskFilters` output, so the "Show sub-tasks" toggle and the
 * agents-working quick filter must be applied per group here. Recomputes
 * each group's total and drops emptied groups. Returns the input by
 * reference when no client filter is active.
 */
export function filterAssigneeGroups(
  groups: TaskAssigneeGroup[] | undefined,
  filters: {
    showSubTasks?: boolean;
    agentRunningFilter?: boolean;
    runningTaskIds?: ReadonlySet<string>;
    propertyFilters?: Record<string, string[]>;
  },
): TaskAssigneeGroup[] | undefined {
  const applyRunning = filters.agentRunningFilter === true;
  const hideSubTasks = filters.showSubTasks === false;
  const hasPropertyFilters = Object.values(filters.propertyFilters ?? {}).some(
    (selected) => selected.length > 0,
  );
  if (!groups || (!applyRunning && !hideSubTasks && !hasPropertyFilters)) return groups;

  const { runningTaskIds } = filters;
  return groups
    .map((group) => {
      const tasks = group.tasks.filter((task) => {
        if (applyRunning && !(runningTaskIds?.has(task.id) ?? false))
          return false;
        if (hideSubTasks && task.parent_task_id) return false;
        if (hasPropertyFilters && !taskMatchesPropertyFilters(task, filters.propertyFilters))
          return false;
        return true;
      });
      return { ...group, tasks, total: tasks.length };
    })
    .filter((group) => group.tasks.length > 0);
}
