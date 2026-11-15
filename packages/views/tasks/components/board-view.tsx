"use client";

import { useState, useCallback, useMemo, useEffect, useRef, memo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import type { QueryKey } from "@tanstack/react-query";
import { arrayMove } from "@dnd-kit/sortable";
import { toast } from "sonner";
import type {
  Task,
  TaskAssigneeGroup,
  TaskAssigneeType,
  TaskStatus,
  Project,
  TaskProperty,
} from "@silieco/core/types";
import { useLoadMoreByAssigneeGroup, useLoadMoreByStatus } from "@silieco/core/tasks/mutations";
import type { AssigneeGroupedTasksFilter, TaskSortParam, MyTasksFilter } from "@silieco/core/tasks/queries";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { propertyIdFromViewKey } from "@silieco/core/tasks/stores/view-store";
import { propertyListOptions, useSetTaskProperty, useUnsetTaskProperty } from "@silieco/core/properties";
import { useWorkspaceId } from "@silieco/core/hooks";
import type { TaskGrouping } from "@silieco/core/tasks/stores/view-store";
import { useActorName } from "@silieco/core/workspace/hooks";
import { BoardColumn, BOARD_CARD_WIDTH, type BoardColumnGroup } from "./board-column";
import { BoardCardContent } from "./board-card";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import { ListLoadMoreFooter } from "./list-load-more-footer";
import type { ChildProgress } from "./list-row";
import type { TaskCreateDefaults } from "../surface/types";
import type {
  TaskStatusPageState,
  TaskStatusPagination,
} from "../surface/use-task-status-branches";
import type {
  TaskGroupBranches,
  TaskGroupPageState,
} from "../surface/use-task-group-branches";
import { useDragSettle } from "./use-drag-settle";
import { useT } from "../../i18n";
import {
  type DragMoveUpdates,
  makeKanbanCollision,
  statusGroupId,
  assigneeGroupId,
  buildColumns,
  computePosition,
  findColumn,
  getMoveAnchors,
  insertIdByPosition,
  taskMatchesGroup,
  getMoveUpdates,
  propertyGroupId,
} from "../utils/drag-utils";

function isStatusGroup(
  group: BoardColumnGroup,
): group is BoardColumnGroup & { status: TaskStatus } {
  return group.status !== undefined;
}

function buildGroups(
  tasks: Task[],
  visibleStatuses: TaskStatus[],
  grouping: TaskGrouping,
  getActorName: (type: string, id: string) => string,
  noAssigneeLabel: string,
  groupingProperty: TaskProperty | null,
  noValueLabel: string,
): BoardColumnGroup[] {
  if (grouping === "status") {
    return visibleStatuses.map((status) => ({
      id: statusGroupId(status),
      title: status,
      status,
      createData: { status },
    }));
  }

  // Select-property board: one column per option (definition order) plus a
  // trailing "No value" column. Empty columns stay visible — they are drop
  // targets for assigning the value.
  if (groupingProperty) {
    const columns: BoardColumnGroup[] = (groupingProperty.config.options ?? []).map(
      (option) => ({
        id: propertyGroupId(groupingProperty.id, option.id),
        title: option.name,
        propertyId: groupingProperty.id,
        propertyOptionId: option.id,
        propertyOptionColor: option.color,
      }),
    );
    columns.push({
      id: propertyGroupId(groupingProperty.id, null),
      title: noValueLabel,
      propertyId: groupingProperty.id,
      propertyOptionId: null,
    });
    return columns;
  }

  const groups = new Map<string, BoardColumnGroup>();
  for (const task of tasks) {
    const id = assigneeGroupId(task.assignee_type, task.assignee_id);
    if (groups.has(id)) continue;

    if (task.assignee_type && task.assignee_id) {
      groups.set(id, {
        id,
        title: getActorName(task.assignee_type, task.assignee_id),
        assigneeType: task.assignee_type,
        assigneeId: task.assignee_id,
        createData: {
          assignee_type: task.assignee_type,
          assignee_id: task.assignee_id,
        },
      });
      continue;
    }

    groups.set(id, {
      id,
      title: noAssigneeLabel,
      assigneeType: null,
      assigneeId: null,
      createData: {
        assignee_type: null,
        assignee_id: null,
      },
    });
  }

  const order: Record<string, number> = {
    member: 0,
    agent: 1,
    squad: 2,
    none: 3,
  };

  return Array.from(groups.values()).toSorted((a, b) => {
    const aOrder = order[a.assigneeType ?? "none"] ?? 99;
    const bOrder = order[b.assigneeType ?? "none"] ?? 99;
    if (aOrder !== bOrder) return aOrder - bOrder;
    return a.title.localeCompare(b.title);
  });
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
const EMPTY_IDS: string[] = [];

function BoardViewImpl({
  tasks,
  assigneeGroups,
  assigneeGroupQueryKey,
  assigneeGroupFilter,
  visibleStatuses,
  hiddenStatuses,
  onMoveTask,
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
  myTasksScope,
  myTasksFilter,
  sort,
  projectId,
  onCreateTask,
  statusPagination,
  groupBranches,
}: {
  tasks: Task[];
  assigneeGroups?: TaskAssigneeGroup[];
  assigneeGroupQueryKey?: QueryKey;
  assigneeGroupFilter?: AssigneeGroupedTasksFilter;
  visibleStatuses: TaskStatus[];
  hiddenStatuses: TaskStatus[];
  onMoveTask: (taskId: string, updates: DragMoveUpdates, onSettled?: () => void) => void;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  /** When set, per-status load-more targets the scoped cache instead of the workspace one. */
  myTasksScope?: string;
  myTasksFilter?: MyTasksFilter;
  /** Must match the sort the page queried with — embedded in the cache key. */
  sort?: TaskSortParam;
  /** When set, the per-column "+" pre-fills the project on the create form. */
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  statusPagination?: TaskStatusPagination;
  groupBranches?: TaskGroupBranches;
}) {
  const { t } = useT("tasks");
  const storeGrouping = useViewStore((s) => s.grouping);
  const sortBy = useViewStore((s) => s.sortBy);
  const boardWsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(boardWsId));
  const groupingPropertyId = propertyIdFromViewKey(storeGrouping);
  const groupingProperty = groupingPropertyId
    ? workspaceProperties.find((p) => p.id === groupingPropertyId && p.type === "select") ?? null
    : null;
  // A persisted `property:<id>` grouping whose definition is gone (archived,
  // deleted, other workspace) falls back to status columns.
  const grouping: TaskGrouping =
    groupingPropertyId && !groupingProperty ? "status" : storeGrouping;
  const groupingOptionIds = useMemo(
    () =>
      groupingProperty
        ? new Set((groupingProperty.config.options ?? []).map((option) => option.id))
        : undefined,
    [groupingProperty],
  );
  const setTaskPropertyMutation = useSetTaskProperty();
  const unsetTaskPropertyMutation = useUnsetTaskProperty();
  const applyPropertyGroupValue = useCallback(
    (group: BoardColumnGroup, taskId: string) => {
      if (group.propertyId === undefined) return;
      // Surface failures like status/assignee drags do (use-task-surface-
      // actions): the mutation rolls the card back, but without a toast the
      // snap-back reads as a UI glitch instead of a rejected write.
      const onError = (err: unknown) => {
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : t(($) => $.page.move_failed),
        );
      };
      if (group.propertyOptionId === null) {
        unsetTaskPropertyMutation.mutate(
          { taskId, propertyId: group.propertyId },
          { onError },
        );
      } else if (group.propertyOptionId !== undefined) {
        setTaskPropertyMutation.mutate(
          {
            taskId,
            propertyId: group.propertyId,
            value: group.propertyOptionId,
          },
          { onError },
        );
      }
    },
    [setTaskPropertyMutation, t, unsetTaskPropertyMutation],
  );
  const sortFieldKey = sortBy === "created_at" ? "created" : sortBy;
  const sortPropertyId = propertyIdFromViewKey(sortBy);
  const sortLabel = sortBy !== "position"
    ? t(($) => $.board.ordered_by, {
        field: sortPropertyId
          ? workspaceProperties.find((p) => p.id === sortPropertyId)?.name ?? ""
          : t(($) => $.display[`sort_${sortFieldKey}` as keyof typeof $.display]),
      })
    : null;
  const { getActorName } = useActorName();
  const myTasksOpts = myTasksScope
    ? { scope: myTasksScope, filter: myTasksFilter ?? {} }
    : undefined;
  const groupedTasks = useMemo(
    () =>
      groupBranches?.enabled
        ? groupBranches.tasks
        : grouping === "assignee" && assigneeGroups
        ? assigneeGroups.flatMap((group) => group.tasks)
        : tasks,
    [assigneeGroups, groupBranches, grouping, tasks],
  );
  const hydratedAssigneeGroups = useMemo<BoardColumnGroup[] | undefined>(() => {
    if (grouping === "assignee" && groupBranches?.enabled) {
      return groupBranches.descriptors.flatMap((descriptor): BoardColumnGroup[] => {
        if (descriptor.value.kind !== "assignee") return [];
        const actorRef = descriptor.value.actor;
        const actor: { type: TaskAssigneeType; id: string } | null =
          actorRef &&
          (actorRef.type === "member" ||
            actorRef.type === "agent" ||
            actorRef.type === "squad")
            ? { type: actorRef.type, id: actorRef.id }
            : null;
        return [{
          id: descriptor.key,
          title: actor
            ? getActorName(actor.type, actor.id)
            : t(($) => $.filters.no_assignee),
          assigneeType: actor?.type ?? null,
          assigneeId: actor?.id ?? null,
          totalCount: descriptor.count,
          createData: {
            assignee_type: actor?.type ?? null,
            assignee_id: actor?.id ?? null,
          },
        }];
      });
    }
    if (grouping !== "assignee" || !assigneeGroups) return undefined;
    const order: Record<string, number> = {
      member: 0,
      agent: 1,
      squad: 2,
      none: 3,
    };
    return assigneeGroups
      .map((group) => ({
        id: group.id,
        title:
          group.assignee_type && group.assignee_id
            ? getActorName(group.assignee_type, group.assignee_id)
            : t(($) => $.filters.no_assignee),
        assigneeType: group.assignee_type,
        assigneeId: group.assignee_id,
        totalCount: group.total,
        createData: {
          assignee_type: group.assignee_type,
          assignee_id: group.assignee_id,
        },
      }))
      .sort((a, b) => {
        const aOrder = order[a.assigneeType ?? "none"] ?? 99;
        const bOrder = order[b.assigneeType ?? "none"] ?? 99;
        if (aOrder !== bOrder) return aOrder - bOrder;
        return a.title.localeCompare(b.title);
      });
  }, [assigneeGroups, getActorName, groupBranches, grouping, t]);
  const groupPagination = useMemo(() => {
    if (!groupBranches?.enabled) return undefined;
    const grouped = new Map<string, TaskGroupPageState[]>();
    for (const descriptor of groupBranches.descriptors) {
      const page = groupBranches.pagination[descriptor.key];
      if (!page) continue;
      let id = descriptor.key;
      if (descriptor.value.kind === "property") {
        const value = descriptor.value;
        id =
          value.value_state === "value" && typeof value.value === "string"
            ? propertyGroupId(value.property_id, value.value)
            : propertyGroupId(value.property_id, null);
      }
      const pages = grouped.get(id) ?? [];
      pages.push(page);
      grouped.set(id, pages);
    }
    return Object.fromEntries(
      Array.from(grouped, ([id, pages]) => [
        id,
        {
          total: pages.reduce((sum, page) => sum + page.total, 0),
          loaded: pages.reduce((sum, page) => sum + page.loaded, 0),
          hasMore: pages.some((page) => page.hasMore),
          isLoading: pages.some((page) => page.isLoading),
          isFetching: pages.some((page) => page.isFetching),
          isError: pages.some((page) => page.isError),
          loadMore: () => {
            for (const page of pages) {
              if (page.hasMore) page.loadMore();
            }
          },
          retry: () => {
            for (const page of pages) {
              if (page.isError) page.retry();
            }
          },
        },
      ]),
    ) as Record<string, TaskGroupPageState>;
  }, [groupBranches]);
  const groups = useMemo(
    () => {
      const built =
        hydratedAssigneeGroups ??
        buildGroups(
        tasks,
        visibleStatuses,
        grouping,
        getActorName,
        t(($) => $.filters.no_assignee),
        groupingProperty,
        t(($) => $.board.no_value),
        );
      return built.map((group) => ({
        ...group,
        totalCount: groupPagination?.[group.id]?.total ?? group.totalCount,
      }));
    },
    [hydratedAssigneeGroups, tasks, visibleStatuses, grouping, getActorName, groupingProperty, groupPagination, t],
  );
  const groupIds = useMemo(
    () => new Set(groups.map((group) => group.id)),
    [groups],
  );
  const groupMap = useMemo(
    () => new Map(groups.map((group) => [group.id, group])),
    [groups],
  );
  const collisionDetection = useMemo(
    () => makeKanbanCollision(groupIds),
    [groupIds],
  );

  // --- Drag state ---
  const [activeTask, setActiveTask] = useState<Task | null>(null);
  // Shared drag/settle primitive: owns the local column mirror, the
  // dragging/settling locks, the post-move animation-frame throttle, and the
  // settle callback. Shared with list-view (and swimlane) so the surfaces
  // can't drift apart. Local columns follow TQ between drags via the resync
  // effect below; during a drag/settle they are frozen by the locks.
  const {
    columns,
    setColumns,
    columnsRef,
    isDraggingRef,
    isSettlingRef,
    recentlyMovedRef,
    settleVersion,
    beginSettle,
  } = useDragSettle(() => buildColumns(groupedTasks, groups, grouping, groupingOptionIds));

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setColumns(buildColumns(groupedTasks, groups, grouping, groupingOptionIds));
    }
  }, [groupedTasks, groups, grouping, groupingOptionIds, settleVersion, setColumns, isDraggingRef, isSettlingRef]);

  // --- Task map ---
  // Frozen during drag so BoardColumn/DraggableBoardCard props stay
  // referentially stable even if a TQ refetch lands mid-drag.
  const taskMap = useMemo(() => {
    const map = new Map<string, Task>();
    for (const task of groupedTasks) map.set(task.id, task);
    return map;
  }, [groupedTasks]);

  const taskMapRef = useRef(taskMap);
  if (!isDraggingRef.current && !isSettlingRef.current) {
    taskMapRef.current = taskMap;
  }

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    })
  );

  const handleDragStart = useCallback(
    (event: DragStartEvent) => {
      isDraggingRef.current = true;
      const task = taskMapRef.current.get(event.active.id as string) ?? null;
      setActiveTask(task);
    },
    [isDraggingRef],
  );

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setColumns((prev) => {
        const activeCol = findColumn(prev, activeId, groupIds);
        const overCol = findColumn(prev, overId, groupIds);
        if (!activeCol || !overCol || activeCol === overCol) return prev;

        if (sortBy !== "position") return prev;

        recentlyMovedRef.current = true;
        const oldIds = prev[activeCol]!.filter((id) => id !== activeId);
        const newIds = [...prev[overCol]!];
        const overIndex = newIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : newIds.length;
        newIds.splice(insertIndex, 0, activeId);
        return { ...prev, [activeCol]: oldIds, [overCol]: newIds };
      });
    },
    [groupIds, sortBy, recentlyMovedRef, setColumns],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveTask(null);

      const resetColumns = () =>
        setColumns(buildColumns(groupedTasks, groups, grouping, groupingOptionIds));

      if (!over) {
        resetColumns();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      const cols = columnsRef.current;
      const activeCol = findColumn(cols, activeId, groupIds);
      const overCol = findColumn(cols, overId, groupIds);
      if (!activeCol || !overCol) {
        resetColumns();
        return;
      }

      // Same-column reorder (manual sort only)
      let finalColumns = cols;
      if (activeCol === overCol && sortBy === "position") {
        const ids = cols[activeCol]!;
        const oldIndex = ids.indexOf(activeId);
        const newIndex = ids.indexOf(overId);
        if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
          const reordered = arrayMove(ids, oldIndex, newIndex);
          finalColumns = { ...cols, [activeCol]: reordered };
          setColumns(finalColumns);
        }
      }

      const finalCol = sortBy === "position"
        ? findColumn(finalColumns, activeId, groupIds)
        : overCol;
      if (!finalCol) {
        resetColumns();
        return;
      }
      const finalGroup = groupMap.get(finalCol);
      if (!finalGroup) {
        resetColumns();
        return;
      }

      const map = taskMapRef.current;

      if (sortBy !== "position") {
        // Cross-column: only update group (status/assignee), keep original position.
        const currentTask = map.get(activeId);
        if (!currentTask || taskMatchesGroup(currentTask, finalGroup)) {
          resetColumns();
          return;
        }
        // Optimistically move the card into the target column *now*. Without
        // this, the sortBy != "position" path never touches local columns on
        // drop, so onDragOver having been a no-op leaves the card in its origin
        // column for the whole request — it only jumps across when the mutation
        // settles. That is the "snaps back to origin, then moves" glitch.
        // Placement mirrors the cache (insertByPosition) so the settle rebuild
        // from TanStack Query is a visual no-op.
        const targetIds = insertIdByPosition(
          (cols[overCol] ?? []).filter((id) => id !== activeId),
          activeId,
          currentTask.position,
          map,
        );
        setColumns((prev) => {
          const fromIds = (prev[activeCol] ?? []).filter((cid) => cid !== activeId);
          return { ...prev, [activeCol]: fromIds, [overCol]: targetIds };
        });
        onMoveTask(
          activeId,
          {
            ...getMoveUpdates(finalGroup, currentTask.position),
            ...getMoveAnchors(targetIds, activeId),
          },
          beginSettle(),
        );
        applyPropertyGroupValue(finalGroup, activeId);
        return;
      }

      const finalIds = finalColumns[finalCol]!;
      const newPosition = computePosition(finalIds, activeId, map);
      const currentTask = map.get(activeId);

      if (
        currentTask &&
        taskMatchesGroup(currentTask, finalGroup) &&
        currentTask.position === newPosition
      ) {
        return;
      }

      // beginSettle() holds the lock and returns the onSettled callback that
      // releases it and resyncs local columns from the cache: a no-op on
      // success (onSuccess already patched the moved card in place), the revert
      // on error (onError restored the snapshot). Without it a failed move would
      // strand the card at the drop target, since onSettled no longer refetches.
      onMoveTask(
        activeId,
        {
          ...getMoveUpdates(finalGroup, newPosition),
          ...getMoveAnchors(finalIds, activeId),
        },
        beginSettle(),
      );
      applyPropertyGroupValue(finalGroup, activeId);
    },
    [groupedTasks, groups, grouping, groupingOptionIds, onMoveTask, groupIds, groupMap, sortBy, beginSettle, columnsRef, isDraggingRef, setColumns, applyPropertyGroupValue],
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div className="flex flex-1 min-h-0 gap-4 overflow-x-auto p-2">
        {groups.length === 0 ? (
          groupBranches?.isError ? (
            <button
              type="button"
              className="flex min-w-full flex-1 items-center justify-center text-body text-destructive hover:underline"
              onClick={groupBranches.retryGroups}
            >
              {t(($) => $.table.load_more_failed_retry)}
            </button>
          ) : (
            <div className="flex min-w-full flex-1 items-center justify-center text-body text-muted-foreground">
              {t(($) => $.board.empty_grouping)}
            </div>
          )
        ) : (
          groups.map((group) =>
            isStatusGroup(group) ? (
              statusPagination ? (
                <ServerPaginatedBoardColumn
                  key={group.id}
                  group={group}
                  taskIds={columns[group.id] ?? EMPTY_IDS}
                  taskMap={taskMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  page={statusPagination[group.status]}
                  projectId={projectId}
                  onCreateTask={onCreateTask}
                  sortLabel={sortLabel}
                />
              ) : (
                <PaginatedBoardColumn
                  key={group.id}
                  group={group}
                  taskIds={columns[group.id] ?? EMPTY_IDS}
                  taskMap={taskMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  myTasksOpts={myTasksOpts}
                  sort={sort}
                  projectId={projectId}
                  onCreateTask={onCreateTask}
                  sortLabel={sortLabel}
                />
              )
            ) : (
              groupPagination?.[group.id] ? (
                <ServerPaginatedBoardColumn
                  key={group.id}
                  group={group}
                  taskIds={columns[group.id] ?? EMPTY_IDS}
                  taskMap={taskMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  page={groupPagination[group.id]!}
                  projectId={projectId}
                  onCreateTask={onCreateTask}
                  sortLabel={sortLabel}
                />
              ) : assigneeGroupQueryKey && assigneeGroupFilter ? (
                <PaginatedAssigneeBoardColumn
                  key={group.id}
                  group={group}
                  taskIds={columns[group.id] ?? EMPTY_IDS}
                  taskMap={taskMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  queryKey={assigneeGroupQueryKey}
                  filter={assigneeGroupFilter}
                  sort={sort}
                  projectId={projectId}
                  onCreateTask={onCreateTask}
                  sortLabel={sortLabel}
                />
              ) : (
                <BoardColumn
                  key={group.id}
                  group={group}
                  taskIds={columns[group.id] ?? EMPTY_IDS}
                  taskMap={taskMapRef.current}
                  childProgressMap={childProgressMap}
                  projectMap={projectMap}
                  projectId={projectId}
                  onCreateTask={onCreateTask}
                  totalCount={group.totalCount}
                  sortLabel={sortLabel}
                />
              )
            ),
          )
        )}
        {groupBranches?.hasMoreGroups && (
          <div className="flex w-8 shrink-0 items-center justify-center">
            <InfiniteScrollSentinel
              onVisible={groupBranches.loadMoreGroups}
              loading={groupBranches.isLoadingMoreGroups}
            />
          </div>
        )}

        {groupingProperty && !groupBranches?.enabled && (
          <PropertyBoardPoolLoader
            statuses={visibleStatuses}
            myTasksOpts={myTasksOpts}
            sort={sort}
          />
        )}

        {grouping === "status" && hiddenStatuses.length > 0 && (
          <BoardHiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            myTasksOpts={myTasksOpts}
            sort={sort}
            statusPagination={statusPagination}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeTask ? (
          <div style={{ width: BOARD_CARD_WIDTH }} className="rotate-1 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
            <BoardCardContent
              task={activeTask}
              childProgress={childProgressMap.get(activeTask.id)}
              project={
                activeTask.project_id
                  ? projectMap?.get(activeTask.project_id)
                  : undefined
              }
            />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}

const PaginatedAssigneeBoardColumn = memo(function PaginatedAssigneeBoardColumn({
  group,
  taskIds,
  taskMap,
  childProgressMap,
  projectMap,
  queryKey,
  filter,
  sort,
  projectId,
  onCreateTask,
  sortLabel,
}: {
  group: BoardColumnGroup;
  taskIds: string[];
  taskMap: Map<string, Task>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  queryKey: QueryKey;
  filter: AssigneeGroupedTasksFilter;
  sort?: TaskSortParam;
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByAssigneeGroup(
    {
      id: group.id,
      assignee_type: group.assigneeType ?? null,
      assignee_id: group.assigneeId ?? null,
    },
    queryKey,
    filter,
    sort,
  );
  return (
    <BoardColumn
      group={group}
      taskIds={taskIds}
      taskMap={taskMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={total}
      projectId={projectId}
      onCreateTask={onCreateTask}
      sortLabel={sortLabel}
      footer={
        <ListLoadMoreFooter
          hasMore={hasMore}
          isLoading={isLoading}
          total={total}
          onLoadMore={loadMore}
        />
      }
    />
  );
});

const ServerPaginatedBoardColumn = memo(function ServerPaginatedBoardColumn({
  group,
  taskIds,
  taskMap,
  childProgressMap,
  projectMap,
  page,
  projectId,
  onCreateTask,
  sortLabel,
}: {
  group: BoardColumnGroup;
  taskIds: string[];
  taskMap: Map<string, Task>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  page: TaskStatusPageState | TaskGroupPageState;
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const footer = (
    <ListLoadMoreFooter
      hasMore={page.hasMore}
      isLoading={page.isLoading || page.isFetching}
      total={page.total}
      onLoadMore={page.loadMore}
      isError={page.isError}
      onRetry={page.retry}
    />
  );
  return (
    <BoardColumn
      group={group}
      taskIds={taskIds}
      taskMap={taskMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={page.total}
      projectId={projectId}
      onCreateTask={onCreateTask}
      sortLabel={sortLabel}
      footer={footer}
    />
  );
});

const PaginatedBoardColumn = memo(function PaginatedBoardColumn({
  group,
  taskIds,
  taskMap,
  childProgressMap,
  projectMap,
  myTasksOpts,
  sort,
  projectId,
  onCreateTask,
  sortLabel,
}: {
  group: BoardColumnGroup & { status: TaskStatus };
  taskIds: string[];
  taskMap: Map<string, Task>;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  sortLabel?: string | null;
}) {
  const { loadMore, hasMore, isLoading, total } = useLoadMoreByStatus(
    group.status,
    myTasksOpts,
    sort,
  );
  return (
    <BoardColumn
      group={group}
      taskIds={taskIds}
      taskMap={taskMap}
      childProgressMap={childProgressMap}
      projectMap={projectMap}
      totalCount={total}
      projectId={projectId}
      onCreateTask={onCreateTask}
      sortLabel={sortLabel}
      footer={
        <ListLoadMoreFooter
          hasMore={hasMore}
          isLoading={isLoading}
          total={total}
          onLoadMore={loadMore}
        />
      }
    />
  );
});

/**
 * Board-view-specific row that pulls the server-aggregated total from
 * `useLoadMoreByStatus` and hands it to the shared {@link HiddenColumnRow}.
 * Lives here (not in `hidden-columns-panel.tsx`) so the shared panel stays
 * free of `useLoadMoreByStatus` / `myTasksOpts` coupling — the swimlane
 * uses an in-memory total instead.
 */
/**
 * The property-grouped board derives its columns from the status-bucketed
 * pool, which pages per status. Property columns have no per-column
 * pagination yet (tracked in SILI-4493), so this strip keeps every task
 * REACHABLE: one sentinel per status that still has server rows loads the
 * pool further and the property columns re-derive. Without it, rows beyond
 * a status's loaded page silently never join any column (review round 3).
 */
function PropertyBoardPoolLoader({
  statuses,
  myTasksOpts,
  sort,
}: {
  statuses: TaskStatus[];
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
}) {
  return (
    <div className="col-span-full flex justify-center py-1">
      {statuses.map((status) => (
        <PropertyBoardPoolSentinel key={status} status={status} myTasksOpts={myTasksOpts} sort={sort} />
      ))}
    </div>
  );
}

function PropertyBoardPoolSentinel({
  status,
  myTasksOpts,
  sort,
}: {
  status: TaskStatus;
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
}) {
  const { loadMore, hasMore, isLoading } = useLoadMoreByStatus(status, myTasksOpts, sort);
  if (!hasMore) return null;
  return <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />;
}

function BoardHiddenColumnRow({
  status,
  myTasksOpts,
  sort,
}: {
  status: TaskStatus;
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
}) {
  const { total } = useLoadMoreByStatus(status, myTasksOpts, sort);
  return <HiddenColumnRow status={status} total={total} />;
}

function BoardHiddenColumnsPanel({
  hiddenStatuses,
  myTasksOpts,
  sort,
  statusPagination,
}: {
  hiddenStatuses: TaskStatus[];
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
  statusPagination?: TaskStatusPagination;
}) {
  return (
    <HiddenColumnsPanel
      hiddenStatuses={hiddenStatuses}
      renderRow={(status) => (
        statusPagination ? (
          <HiddenColumnRow
            key={status}
            status={status}
            total={statusPagination[status].total}
          />
        ) : (
          <BoardHiddenColumnRow
            key={status}
            status={status}
            myTasksOpts={myTasksOpts}
            sort={sort}
          />
        )
      )}
    />
  );
}

/**
 * Memoized: the surface controller re-renders on loading-flag flips (e.g. a
 * query enabling when the view changes) — without memo every such flip
 * re-rendered this entire view tree (hundreds of ms). All props are
 * referentially stable useMemo/useCallback outputs from the controller.
 */
export const BoardView = memo(BoardViewImpl);
