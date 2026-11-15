"use client";

import { useCallback, useMemo } from "react";
import {
  useQuery,
  type QueryKey,
} from "@tanstack/react-query";
import type { Task, TaskAssigneeGroup, Project } from "@silieco/core/types";
import { ALL_STATUSES } from "@silieco/core/tasks/config";
import { projectListOptions } from "@silieco/core/projects/queries";
import {
  childTaskProgressOptions,
  type AssigneeGroupedTasksFilter,
  type TaskSortParam,
  type MyTasksFilter,
} from "@silieco/core/tasks/queries";
import {
  taskSurfaceAssigneeGroupsOptions,
  taskSurfaceGanttOptions,
  taskSurfaceListOptions,
} from "@silieco/core/tasks/surface/repository";
import type { TaskSurfaceQueryPlan } from "@silieco/core/tasks/surface/query-plan";
import type { TaskStatus } from "@silieco/core/types";
import {
  applyTaskFilters,
  filterAssigneeGroups,
  type TaskFilterState,
  type TaskFilters,
} from "../utils/filter";
import type { ChildProgress } from "../components/list-row";
import type {
  TaskStatusBranches,
  TaskStatusPagination,
} from "./use-task-status-branches";
import type { TaskGroupBranches } from "./use-task-group-branches";

const EMPTY_TASKS: Task[] = [];
const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();
const EMPTY_PROJECTS: Project[] = [];

/**
 * The rows the gantt canvas actually draws, on top of the shared filters.
 *
 * The canvas adds two rules of its own: a row needs a date to be placed, and
 * completed work is hidden unless the user asks for it. The data source only
 * delivers scheduled tasks (server-side `scheduled=true`), but a row can
 * still arrive without a date — e.g. a WS-driven optimistic patch that just
 * cleared start_date / due_date and is waiting for the cache to refetch — so
 * the date check stays defensive.
 *
 * These rules live HERE rather than privately inside GanttView so the header
 * chip can narrow the same set the canvas draws. A view that filters its own
 * rows in secret is exactly how the chip's count drifted from the list in the
 * first place (SILI-4884); duplicating the rules in both places would just
 * reintroduce the drift with extra steps.
 */
function ganttCanvasRows(tasks: Task[], showCompleted: boolean): Task[] {
  const dated = tasks.filter((i) => i.start_date || i.due_date);
  if (showCompleted) return dated;
  return dated.filter((i) => i.status !== "done" && i.status !== "cancelled");
}

export interface TaskSurfaceData {
  surfaceTasks: Task[];
  projectTasks: Task[];
  tasks: Task[];
  swimlaneTasks: Task[];
  /** The rows the agents-working filter would leave on screen. `undefined`
   *  means the set is genuinely unknown: Table membership is server-owned,
   *  and the activity chip must not reconstruct a complete task window just
   *  to decorate the header. */
  workingScopeTasks: Task[] | undefined;
  filteredGanttTasks: Task[];
  assigneeGroups?: TaskAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedTasksFilter;
  filter: MyTasksFilter;
  loadMoreScope?: string;
  loadMoreFilter?: MyTasksFilter;
  ganttTasks: Task[];
  visibleStatuses: TaskStatus[];
  hiddenStatuses: TaskStatus[];
  statusPagination: TaskStatusPagination;
  activeFilters: Omit<TaskFilters, "statusFilters">;
  childProgressMap: Map<string, ChildProgress>;
  projectMap: Map<string, Project>;
  resolveTableExportLookups: (needs: {
    projects: boolean;
    childProgress: boolean;
  }) => Promise<{
    projectMap: Map<string, Project>;
    childProgressMap: Map<string, ChildProgress>;
  }>;
  isLoading: boolean;
  /** The window's data is being revalidated while the previous snapshot is
   *  shown as a placeholder (sort/date change, or any grouped-board filter
   *  change). Drives the header's deferred refresh indicator — content stays
   *  put, so this is NOT a loading state. */
  isRefreshing: boolean;
  isEmpty: boolean;
}

export function useTaskSurfaceData({
  wsId,
  queryPlan,
  projectId,
  usesAssigneeBoard,
  usesGantt,
  usesTable,
  serverStatusBranches,
  serverGroupBranches,
  ganttShowCompleted,
  sort,
  statusFilters,
  priorityFilters,
  assigneeFilters,
  includeNoAssignee,
  agentRunningFilter,
  creatorFilters,
  projectFilters,
  includeNoProject,
  labelFilters,
  propertyFilters,
  workingTaskIDs,
  showSubTasks,
  loadProjects,
}: {
  wsId: string;
  queryPlan: TaskSurfaceQueryPlan;
  projectId?: string;
  usesAssigneeBoard: boolean;
  usesGantt: boolean;
  usesTable: boolean;
  serverStatusBranches: TaskStatusBranches;
  serverGroupBranches: TaskGroupBranches;
  /** Gantt's "show completed" display toggle. The canvas hides done/cancelled
   *  rows without it, so the working scope has to honour it too. */
  ganttShowCompleted: boolean;
  sort: TaskSortParam;
  statusFilters: TaskStatus[];
  priorityFilters: TaskFilterState["priorityFilters"];
  assigneeFilters: TaskFilterState["assigneeFilters"];
  includeNoAssignee: boolean;
  agentRunningFilter: boolean;
  creatorFilters: TaskFilterState["creatorFilters"];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters: Record<string, string[]>;
  /** Distinct running-task task ids projected by `/api/working-agents`. */
  workingTaskIDs: ReadonlySet<string>;
  showSubTasks: boolean;
  loadProjects: boolean;
}): TaskSurfaceData {
  const assigneeGroupFilter = useMemo<AssigneeGroupedTasksFilter>(
    () => ({
      ...queryPlan.groupedScopeFilter,
      statuses: statusFilters.length > 0 ? statusFilters : [...ALL_STATUSES],
      priorities: priorityFilters,
      assignee_filters: assigneeFilters,
      include_no_assignee: includeNoAssignee,
      creator_filters: creatorFilters,
      project_ids: projectFilters,
      include_no_project: includeNoProject,
      label_ids: labelFilters,
    }),
    [
      assigneeFilters,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      queryPlan.groupedScopeFilter,
      statusFilters,
    ],
  );

  const activeAssigneeGroupsOptions = taskSurfaceAssigneeGroupsOptions(
    wsId,
    queryPlan,
    assigneeGroupFilter,
    sort,
  );

  const statusTasksQuery = useQuery({
    ...taskSurfaceListOptions(wsId, queryPlan, sort),
    enabled:
      !usesAssigneeBoard &&
      !usesGantt &&
      !usesTable &&
      !serverStatusBranches.enabled &&
      !serverGroupBranches.enabled,
  });
  const assigneeGroupsQuery = useQuery({
    ...activeAssigneeGroupsOptions,
    enabled: usesAssigneeBoard && !serverGroupBranches.enabled,
  });
  const ganttTasksQuery = useQuery({
    ...taskSurfaceGanttOptions(wsId, projectId ?? ""),
    enabled: usesGantt,
  });
  const hasWorkingTasks = workingTaskIDs.size > 0;
  const workingFilterContext = useMemo(
    () => ({ runningTaskIds: workingTaskIDs }),
    [workingTaskIDs],
  );
  const bucketedTasks = useMemo(() => {
    return serverStatusBranches.enabled
      ? serverStatusBranches.tasks
      : serverGroupBranches.enabled
      ? serverGroupBranches.tasks
      : usesAssigneeBoard
      ? (assigneeGroupsQuery.data?.groups.flatMap((group) => group.tasks) ?? [])
      : (statusTasksQuery.data ?? EMPTY_TASKS);
  }, [
    assigneeGroupsQuery.data?.groups,
    serverStatusBranches.enabled,
    serverStatusBranches.tasks,
    serverGroupBranches.enabled,
    serverGroupBranches.tasks,
    statusTasksQuery.data,
    usesAssigneeBoard,
  ]);

  // `cancelled` is a first-class default status (SILI-4290): it is fetched into
  // the cache like every other status and flows straight through to list /
  // board / swimlane columns, header facet counts, batch selection, and the
  // isEmpty check. The status filter narrows this set like any other status —
  // it no longer unlocks an otherwise-hidden bucket.
  const ganttTasks = ganttTasksQuery.data ?? EMPTY_TASKS;
  const surfaceTasks = usesGantt
    ? ganttTasks
    : usesTable
      ? EMPTY_TASKS
      : bucketedTasks;

  const baseFilterState = useMemo<TaskFilterState>(
    () => ({
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      workingOnly: agentRunningFilter,
      showSubTasks,
    }),
    [
      assigneeFilters,
      agentRunningFilter,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      priorityFilters,
      projectFilters,
      propertyFilters,
      showSubTasks,
      statusFilters,
    ],
  );

  const tasks = useMemo(
    () =>
      serverStatusBranches.enabled
        ? surfaceTasks
        : applyTaskFilters(
            surfaceTasks,
            baseFilterState,
            workingFilterContext,
          ),
    [
      baseFilterState,
      serverStatusBranches.enabled,
      surfaceTasks,
      workingFilterContext,
    ],
  );

  const statuslessFilterState = useMemo<TaskFilterState>(
    () => ({
      ...baseFilterState,
      statusFilters: [],
    }),
    [baseFilterState],
  );

  const swimlaneTasks = useMemo(
    () =>
      applyTaskFilters(
        surfaceTasks,
        statuslessFilterState,
        workingFilterContext,
      ),
    [statuslessFilterState, surfaceTasks, workingFilterContext],
  );

  const filteredGanttTasks = useMemo(
    () =>
      ganttCanvasRows(
        applyTaskFilters(ganttTasks, baseFilterState, workingFilterContext),
        ganttShowCompleted,
      ),
    [
      baseFilterState,
      ganttTasks,
      ganttShowCompleted,
      workingFilterContext,
    ],
  );

  // The assignee-grouped board renders straight from `groups`, bypassing the
  // flat applyTaskFilters output — re-apply the remaining client-only
  // display filters per group. Server-owned group paths encode running-task
  // membership in the canonical query; this fallback uses the same task ids.
  const filteredAssigneeGroups = useMemo(
    () =>
      filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
        agentRunningFilter,
        runningTaskIds: workingTaskIDs,
        showSubTasks,
        propertyFilters,
      }),
    [
      assigneeGroupsQuery.data?.groups,
      agentRunningFilter,
      propertyFilters,
      showSubTasks,
      workingTaskIDs,
    ],
  );

  const workingFilterState = useMemo<TaskFilterState>(
    () => ({
      ...baseFilterState,
      workingOnly: true,
    }),
    [baseFilterState],
  );

  // The rows the agents-working filter leaves on screen — i.e. exactly what
  // you get when you click the header chip.
  //
  // This is deliberately a projection of the render pipeline. The controller
  // translates `/api/working-agents` into running task ids once; both the
  // canonical server query and client-only Gantt/extra-child paths reuse that
  // returned task-id set.
  //
  // The chip counts AGENTS, not this list's length, so these are not equal
  // (one agent can hold two of these rows). What this set does decide is
  // WHICH agents the chip counts — only those working on rows that survive
  // the filters. Re-deriving that scope from the snapshot instead is what
  // made the chip disagree with the list it was filtering: any active
  // status/assignee/label filter, or a sub-task hidden by the display
  // toggle, moved the list but not the chip (SILI-4884).
  //
  // Each branch below must take the SAME source the matching branch of
  // TaskSurface renders:
  //   - gantt          → the canvas set (scheduled + dated + showCompleted)
  //   - assignee board → the grouped response, not the flat list
  //   - table          → unknown unless the running set is empty; Table uses
  //     server cursor branches and never materializes a second full window
  //   - board / list / swimlane → the flat filtered list
  //
  // Swimlane deliberately has no branch: SwimLaneView draws its cards from
  // `tasks` (status filter applied) and only uses the statusless
  // `swimlaneTasks` for LANE DISCOVERY, so scoping the chip to the
  // statusless set would count rows the canvas never draws.
  const workingScopeTasks = useMemo(() => {
    if (usesGantt) {
      return ganttCanvasRows(
        applyTaskFilters(
          ganttTasks,
          workingFilterState,
          workingFilterContext,
        ),
        ganttShowCompleted,
      );
    }
    if (usesAssigneeBoard && !serverGroupBranches.enabled) {
      const groupedTasks = (
        filterAssigneeGroups(assigneeGroupsQuery.data?.groups, {
          agentRunningFilter: true,
          runningTaskIds: workingTaskIDs,
          showSubTasks,
          propertyFilters,
        }) ?? []
      ).flatMap((group) => group.tasks);
      return applyTaskFilters(
        groupedTasks,
        workingFilterState,
        workingFilterContext,
      );
    }
    if (usesTable || serverStatusBranches.enabled || serverGroupBranches.enabled) {
      // Table membership is server-owned and cursor paged. Do not rebuild a
      // second complete task window merely to decorate the activity chip:
      // that was the final hidden auto-materialization loop behind the old
      // 1,000-row ceiling. An empty running-task set is trivially
      // known; otherwise keep the chip indeterminate until a bounded server
      // facet supplies the matching task/task projection.
      if (!hasWorkingTasks) return EMPTY_TASKS;
      return undefined;
    }
    return applyTaskFilters(
      surfaceTasks,
      workingFilterState,
      workingFilterContext,
    );
  }, [
    assigneeGroupsQuery.data?.groups,
    ganttTasks,
    ganttShowCompleted,
    hasWorkingTasks,
    propertyFilters,
    showSubTasks,
    surfaceTasks,
    usesAssigneeBoard,
    usesGantt,
    usesTable,
    serverStatusBranches.enabled,
    serverGroupBranches.enabled,
    workingFilterState,
    workingFilterContext,
    workingTaskIDs,
  ]);

  const {
    data: childProgressData,
    refetch: refetchChildProgress,
  } = useQuery(childTaskProgressOptions(wsId));
  const childProgressMap = childProgressData ?? EMPTY_CHILD_PROGRESS;
  const {
    data: projectData,
    refetch: refetchProjects,
  } = useQuery({
    ...projectListOptions(wsId),
    enabled: loadProjects,
  });
  const projects = projectData ?? EMPTY_PROJECTS;
  const projectMap = useMemo(
    () => new Map(projects.map((project) => [project.id, project])),
    [projects],
  );
  const resolveTableExportLookups = useCallback(
    async (needs: { projects: boolean; childProgress: boolean }) => {
      const [projectResult, progressResult] = await Promise.all([
        needs.projects ? refetchProjects() : Promise.resolve(null),
        needs.childProgress
          ? refetchChildProgress()
          : Promise.resolve(null),
      ]);
      if (projectResult?.error) throw projectResult.error;
      if (progressResult?.error) throw progressResult.error;
      if (needs.projects && !projectResult?.data) {
        throw new Error("Failed to load project data for export");
      }
      if (needs.childProgress && !progressResult?.data) {
        throw new Error("Failed to load child progress for export");
      }
      const resolvedProjects = projectResult?.data ?? projects;
      return {
        projectMap: new Map(
          resolvedProjects.map((project) => [project.id, project]),
        ),
        childProgressMap: progressResult?.data ?? childProgressMap,
      };
    },
    [
      childProgressMap,
      projects,
      refetchChildProgress,
      refetchProjects,
    ],
  );

  const visibleStatuses = useMemo<TaskStatus[]>(() => {
    // Default view shows every lifecycle status, `cancelled` last (its
    // canonical position in ALL_STATUSES). An active status filter narrows to
    // the selected subset while preserving that order.
    if (statusFilters.length > 0) {
      return ALL_STATUSES.filter((s) => statusFilters.includes(s));
    }
    return ALL_STATUSES;
  }, [statusFilters]);

  // Hidden columns are the lifecycle statuses not currently visible, so
  // `cancelled` participates in the board show/hide controls exactly like the
  // rest of the statuses.
  const hiddenStatuses = useMemo<TaskStatus[]>(
    () => ALL_STATUSES.filter((s) => !visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const activeFilters = useMemo(
    () => ({
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      agentRunningFilter,
      runningTaskIds: workingTaskIDs,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
      propertyFilters,
      showSubTasks,
    }),
    [
      assigneeFilters,
      agentRunningFilter,
      creatorFilters,
      includeNoAssignee,
      includeNoProject,
      labelFilters,
      propertyFilters,
      priorityFilters,
      projectFilters,
      showSubTasks,
      workingTaskIDs,
    ],
  );

  const isLoading = serverGroupBranches.enabled
    ? serverGroupBranches.isLoading
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isLoading
      : usesGantt
      ? ganttTasksQuery.isLoading
      : usesTable
        ? false
        : serverStatusBranches.enabled
          ? serverStatusBranches.isLoading
          : statusTasksQuery.isLoading;

  // Placeholder-backed revalidation of the ACTIVE query only. First loads are
  // isLoading (no previous data to place-hold); gantt has no placeholder
  // phase (its key carries no sort/filter).
  const isRefreshing = serverGroupBranches.enabled
    ? serverGroupBranches.isRefreshing
    : usesAssigneeBoard
      ? assigneeGroupsQuery.isPlaceholderData
      : usesGantt
      ? false
      : usesTable
        ? false
        : serverStatusBranches.enabled
          ? serverStatusBranches.isRefreshing
          : statusTasksQuery.isPlaceholderData;

  return {
    surfaceTasks,
    projectTasks: surfaceTasks,
    tasks,
    swimlaneTasks,
    workingScopeTasks,
    filteredGanttTasks,
    assigneeGroups: usesAssigneeBoard ? filteredAssigneeGroups : undefined,
    assigneeGroupQueryKey: usesAssigneeBoard
      ? activeAssigneeGroupsOptions.queryKey
      : undefined,
    assigneeGroupFilter: usesAssigneeBoard ? assigneeGroupFilter : undefined,
    filter: queryPlan.queryFilter,
    loadMoreScope: queryPlan.loadMoreScope,
    loadMoreFilter: queryPlan.loadMoreFilter,
    ganttTasks,
    visibleStatuses,
    hiddenStatuses,
    statusPagination: serverStatusBranches.pagination,
    activeFilters,
    childProgressMap,
    projectMap,
    resolveTableExportLookups,
    isLoading,
    isRefreshing,
    // isEmpty asserts "this window has no tasks". The board/list/swimlane
    // data IS the full window, so an empty result proves it. The gantt query
    // is a scheduled-only PROJECTION — an empty subset cannot prove the
    // window is empty, so never claim it (same "uncertain → don't assert"
    // rule as surface membership). GanttView renders its own accurate
    // "no scheduled tasks" empty state instead of the generic create-task
    // one. Table owns its own branch-level loading, empty and retry states,
    // so this shared legacy surface projection never asserts Table empty.
    isEmpty:
      !isLoading &&
      !usesGantt &&
      !usesTable &&
      (serverStatusBranches.enabled
        ? serverStatusBranches.isTotalKnown &&
          serverStatusBranches.total === 0
        : serverGroupBranches.enabled
          ? !serverGroupBranches.isError &&
            serverGroupBranches.total === 0
        : surfaceTasks.length === 0),
  };
}
