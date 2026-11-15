"use client";

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { ListTodo, Plus } from "lucide-react";
import { Button } from "@silieco/ui/components/ui/button";
import { Skeleton } from "@silieco/ui/components/ui/skeleton";
import { cn } from "@silieco/ui/lib/utils";
import { useWorkspaceId } from "@silieco/core/hooks";
import { ViewStoreProvider } from "@silieco/core/tasks/stores/view-store-context";
import { getTaskSurfaceViewStore } from "@silieco/core/tasks/stores/surface-view-store";
import { taskScopeKey } from "@silieco/core/tasks/surface/scope";
import type { Task } from "@silieco/core/types";
import { BoardView } from "../components/board-view";
import { BatchActionToolbar } from "../components/batch-action-toolbar";
import { GanttView } from "../components/gantt-view";
import { TasksHeader } from "../components/tasks-header";
import { ListView } from "../components/list-view";
import { SwimLaneView } from "../components/swimlane-view";
import { TableView } from "../components/table-view";
import { useT } from "../../i18n";
import { TaskContextMenuProvider } from "../actions";
import { TaskSurfaceActionsProvider } from "./actions-context";
import { TaskSurfaceSelectionProvider } from "./selection-context";
import type { TaskCreateDefaults, TaskSurfaceProps } from "./types";
import {
  useTaskSurfaceController,
  type TaskSurfaceController,
} from "./use-task-surface-controller";

export interface TaskSurfaceRenderContext {
  controller: TaskSurfaceController;
  tasks: Task[];
  /** The rows the agents-working filter would leave on screen, with this
   *  surface's `clientFilter` applied — headers feed it to the working chip
   *  so the chip's count is the post-click row count (SILI-4884). Undefined
   *  means the set is UNKNOWN (not materialized by the server-backed Table);
   *  the chip renders an indeterminate state instead of a number. */
  workingTasks: Task[] | undefined;
}

interface TaskSurfaceComponentProps extends TaskSurfaceProps {
  renderHeader?: (context: TaskSurfaceRenderContext) => ReactNode;
  renderEmpty?: (context: TaskSurfaceRenderContext) => ReactNode;
  renderLoading?: (context: TaskSurfaceRenderContext) => ReactNode;
  clientFilter?: (task: Task) => boolean;
  showClientEmpty?: (context: TaskSurfaceRenderContext) => boolean;
  batchToolbar?: "always" | "list" | "never";
  contentClassName?: string;
}

export function TaskSurface({
  scope,
  modes,
  surfaceKey,
  createDefaults,
  search,
  renderHeader,
  renderEmpty,
  renderLoading,
  clientFilter,
  showClientEmpty,
  batchToolbar = "always",
  contentClassName,
}: TaskSurfaceComponentProps) {
  const wsId = useWorkspaceId();
  const resolvedSurfaceKey = surfaceKey ?? taskScopeKey(scope);
  const store = useMemo(
    () => getTaskSurfaceViewStore(resolvedSurfaceKey),
    [resolvedSurfaceKey],
  );

  // Every change of this key tears down and remounts the ENTIRE surface
  // (providers, DnD, all columns/cards) — by design for data-window changes,
  // but expensive enough that unexpected flips are performance bugs. Dev-only
  // breadcrumb so a Performance trace showing double mounts can be tied to
  // the exact key transition.
  const contentKey = `${wsId}:${taskScopeKey(scope)}`;
  useEffect(() => {
    if (process.env.NODE_ENV !== "production") {
      console.warn(`[task-surface] mount ${contentKey}`);
    }
  }, [contentKey]);

  return (
    <ViewStoreProvider store={store}>
      {/* Remount on data-window change: the list queries keep the previous
          key's data as a placeholder (keepPreviousData) so sort/filter
          changes within ONE surface never flash a skeleton — but reusing the
          mounted observer across windows made project A's cards impersonate
          project B (with isLoading=false, so no skeleton either) until B's
          fetch landed. A window-keyed remount gives the new window a fresh
          observer: cold window → skeleton, warm window → instant cache hit.
          The window identity is wsId + scope — wsId is required because the
          workspace layout does not remount on workspace switch and two
          workspaces share the same scope key (e.g. "workspace:all"). Keyed
          by data identity, not surfaceKey (view-preference identity). */}
      <TaskSurfaceContent
        key={contentKey}
        scope={scope}
        modes={modes}
        createDefaults={createDefaults}
        search={search}
        renderHeader={renderHeader}
        renderEmpty={renderEmpty}
        renderLoading={renderLoading}
        clientFilter={clientFilter}
        showClientEmpty={showClientEmpty}
        batchToolbar={batchToolbar}
        contentClassName={contentClassName}
      />
    </ViewStoreProvider>
  );
}

function TaskSurfaceContent({
  scope,
  modes,
  createDefaults,
  search,
  renderHeader,
  renderEmpty,
  renderLoading,
  clientFilter,
  showClientEmpty,
  batchToolbar,
  contentClassName,
}: Omit<TaskSurfaceComponentProps, "surfaceKey">) {
  const { t } = useT("projects");
  const controller = useTaskSurfaceController({
    scope,
    modes,
    createDefaults,
    search,
  });
  const [tableLoadedTasks, setTableLoadedTasks] = useState<Task[]>([]);
  const handleTableLoadedTasksChange = useCallback((next: Task[]) => {
    setTableLoadedTasks((current) =>
      current.length === next.length &&
      current.every((task, index) => task === next[index])
        ? current
        : next,
    );
  }, []);
  useEffect(() => {
    if (controller.viewMode !== "table") setTableLoadedTasks([]);
  }, [controller.viewMode]);
  const tasks = useMemo(
    () =>
      clientFilter
        ? controller.tasks.filter((task) => clientFilter(task))
        : controller.tasks,
    [clientFilter, controller.tasks],
  );
  const swimlaneTasks = useMemo(
    () =>
      clientFilter
        ? controller.swimlaneTasks.filter((task) => clientFilter(task))
        : controller.swimlaneTasks,
    [clientFilter, controller.swimlaneTasks],
  );
  // Same clientFilter the rendered rows go through, so the chip's promise
  // survives on surfaces that narrow the list locally (e.g. a search box).
  // An UNKNOWN scope (undefined) passes through untouched — there is nothing
  // to filter and the chip must see it as unknown.
  const workingTasks = useMemo(
    () =>
      clientFilter && controller.workingScopeTasks
        ? controller.workingScopeTasks.filter((task) => clientFilter(task))
        : controller.workingScopeTasks,
    [clientFilter, controller.workingScopeTasks],
  );
  const renderContext = useMemo(
    () => ({ controller, tasks, workingTasks }),
    [controller, tasks, workingTasks],
  );
  const openCreateTask = useCallback(
    (defaults?: TaskCreateDefaults) => {
      controller.openCreateTask(defaults);
    },
    [controller],
  );
  // Stable reference for BoardView's tasks: the inline flatMap allocated a
  // fresh array every render, defeating BoardView's memo.
  const boardTasks = useMemo(
    () =>
      controller.assigneeGroups
        ? controller.assigneeGroups.flatMap((group) => group.tasks)
        : tasks,
    [controller.assigneeGroups, tasks],
  );
  const shouldShowClientEmpty =
    !!clientFilter &&
    tasks.length === 0 &&
    (showClientEmpty ? showClientEmpty(renderContext) : true);
  const shouldShowBatchToolbar =
    batchToolbar !== "never" &&
    (batchToolbar === "always" ||
      controller.viewMode === "list" ||
      controller.viewMode === "table");

  return (
    <TaskSurfaceActionsProvider actions={controller.actions}>
      {/* One shared right-click menu for every card/row this surface renders
          — see TaskContextMenuProvider. Inside the actions provider so the
          singleton's useTaskActions routes updates through surface
          actions. */}
      <TaskContextMenuProvider>
      <TaskSurfaceSelectionProvider selection={controller.selection}>
        {renderHeader ? (
          renderHeader(renderContext)
        ) : (
          <TasksHeader
            scopedTasks={controller.surfaceTasks}
            allowGantt={controller.allowGantt}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={
              controller.facetCountsExact
            }
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        {controller.isLoading ? (
          renderLoading ? (
            renderLoading(renderContext)
          ) : (
            <TaskSurfaceSkeleton mode={controller.viewMode} />
          )
        ) : controller.isEmpty || shouldShowClientEmpty ? (
          renderEmpty ? (
            renderEmpty(renderContext)
          ) : (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-body">{t(($) => $.detail.empty_tasks_title)}</p>
              <p className="text-caption">{t(($) => $.detail.empty_tasks_hint)}</p>
              <Button
                variant="outline"
                size="sm"
                className="mt-1"
                onClick={() => controller.openCreateTask()}
              >
                <Plus className="size-3.5 mr-1.5" />
                {t(($) => $.detail.empty_tasks_new_button)}
              </Button>
            </div>
          )
        ) : (
          <div className={cn("flex flex-col flex-1 min-h-0", contentClassName)}>
            {controller.viewMode === "board" && (
              <BoardView
                tasks={boardTasks}
                assigneeGroups={controller.assigneeGroups}
                assigneeGroupQueryKey={controller.assigneeGroupQueryKey}
                assigneeGroupFilter={controller.assigneeGroupFilter}
                visibleStatuses={controller.visibleStatuses}
                hiddenStatuses={controller.hiddenStatuses}
                onMoveTask={controller.moveTask}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                myTasksScope={controller.loadMoreScope}
                myTasksFilter={controller.loadMoreFilter}
                sort={controller.sort}
                projectId={controller.projectId}
                onCreateTask={openCreateTask}
                statusPagination={controller.statusPagination}
                groupBranches={controller.groupBranches}
              />
            )}
            {controller.viewMode === "list" && (
              <ListView
                tasks={tasks}
                visibleStatuses={controller.visibleStatuses}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                projectId={controller.projectId}
                onMoveTask={controller.moveTask}
                onCreateTask={openCreateTask}
                statusPagination={controller.statusPagination!}
              />
            )}
            {controller.viewMode === "table" && (
              <TableView
                serverQuery={controller.tableQuerySpec}
                childProgressMap={controller.childProgressMap}
                search={controller.tableSearch}
                onSearchChange={controller.setTableSearch}
                onLoadedTasksChange={handleTableLoadedTasksChange}
                onCreateTask={openCreateTask}
                exportTasks={controller.exportTableTasks}
                resolveExportLookups={controller.resolveTableExportLookups}
              />
            )}
            {controller.viewMode === "gantt" && (
              <GanttView tasks={controller.filteredGanttTasks} />
            )}
            {controller.viewMode === "swimlane" && (
              <SwimLaneView
                tasks={tasks}
                unfilteredTasks={swimlaneTasks}
                activeFilters={controller.activeFilters}
                visibleStatuses={controller.visibleStatuses}
                hiddenStatuses={controller.hiddenStatuses}
                onMoveTask={controller.moveTask}
                childProgressMap={controller.childProgressMap}
                projectMap={controller.projectMap}
                myTasksScope={controller.loadMoreScope}
                myTasksFilter={controller.loadMoreFilter}
                sort={controller.sort}
                projectId={controller.projectId}
                onCreateTask={openCreateTask}
                groupBranches={controller.groupBranches}
              />
            )}
          </div>
        )}
        {shouldShowBatchToolbar && (
          <BatchActionToolbar
            tasks={
              controller.viewMode === "table" ? tableLoadedTasks : tasks
            }
          />
        )}
      </TaskSurfaceSelectionProvider>
      </TaskContextMenuProvider>
    </TaskSurfaceActionsProvider>
  );
}

// Table is deliberately absent. It owns its own placeholders, drawn as rows
// inside its real grid so the header, column widths and toolbar are up before
// any data is — a surface-level stand-in would replace all of that with bars
// of a different shape and then jump when the rows arrived.
function TaskSurfaceSkeleton({ mode }: { mode: string }) {
  if (mode === "list") {
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-10 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-4">
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="flex min-w-52 flex-1 flex-col gap-2">
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-24 w-full rounded-lg" />
          <Skeleton className="h-24 w-full rounded-lg" />
        </div>
      ))}
    </div>
  );
}
