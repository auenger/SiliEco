import type { UseQueryOptions } from "@tanstack/react-query";
import {
  taskAssigneeGroupsOptions,
  taskFlatExportOptions,
  taskFlatListOptions,
  taskListOptions,
  myTaskAssigneeGroupsOptions,
  myTaskListOptions,
  projectGanttTasksOptions,
  type AssigneeGroupedTasksFilter,
  type TaskFlatFilter,
  type TaskSortParam,
} from "../queries";
import type {
  GroupedTasksResponse,
  Task,
  ListTasksCache,
} from "../../types";
import type { TaskSurfaceQueryPlan } from "./query-plan";

/**
 * Task surface repository — resolves a {@link TaskSurfaceQueryPlan} to the
 * concrete query options backing each render mode. Views declare WHICH
 * window they show (scope → plan) and WHICH mode is active; which endpoint,
 * cache key, and pagination style serve it is decided here in core, so the
 * view layer never branches on workspace-vs-scoped plumbing.
 */

/** Status-bucketed list — feeds the board / list / swimlane modes.
 *
 * Both branches share the exact cache shape (`ListTasksCache` selected to
 * `Task[]`); the cast below only erases the branch-specific query-key
 * literal so one `useQuery` call site can consume either plan kind. */
export function taskSurfaceListOptions(
  wsId: string,
  plan: TaskSurfaceQueryPlan,
  sort?: TaskSortParam,
): UseQueryOptions<ListTasksCache, Error, Task[]> {
  return (
    plan.kind === "workspace"
      ? taskListOptions(wsId, sort)
      : myTaskListOptions(wsId, plan.queryScope, plan.queryFilter, plan.userId, sort)
  ) as UseQueryOptions<ListTasksCache, Error, Task[]>;
}

/** Flat cross-status window — feeds the table mode with offset pagination. */
export function taskSurfaceFlatOptions(
  wsId: string,
  plan: TaskSurfaceQueryPlan,
  sort?: TaskSortParam,
  facets: TaskFlatFilter = {},
) {
  return taskFlatListOptions(
    wsId,
    plan.queryScope ?? plan.scopeKey,
    { ...plan.queryFilter, ...facets },
    plan.userId,
    sort,
  );
}

/** Fully materialized flat window used only for an explicit CSV export. */
export function taskSurfaceFlatExportOptions(
  wsId: string,
  plan: TaskSurfaceQueryPlan,
  sort?: TaskSortParam,
  facets: TaskFlatFilter = {},
) {
  return taskFlatExportOptions(
    wsId,
    plan.queryScope ?? plan.scopeKey,
    { ...plan.queryFilter, ...facets },
    plan.userId,
    sort,
  );
}

/** Assignee-grouped list — feeds the board's group-by-assignee mode. */
export function taskSurfaceAssigneeGroupsOptions(
  wsId: string,
  plan: TaskSurfaceQueryPlan,
  filter: AssigneeGroupedTasksFilter,
  sort?: TaskSortParam,
): UseQueryOptions<GroupedTasksResponse> {
  return (
    plan.kind === "workspace"
      ? taskAssigneeGroupsOptions(wsId, filter, sort)
      : myTaskAssigneeGroupsOptions(wsId, plan.queryScope, filter, plan.userId, sort)
  ) as UseQueryOptions<GroupedTasksResponse>;
}

/** Scheduled-only task set — feeds a project surface's Gantt mode. */
export function taskSurfaceGanttOptions(wsId: string, projectId: string) {
  return projectGanttTasksOptions(wsId, projectId);
}
