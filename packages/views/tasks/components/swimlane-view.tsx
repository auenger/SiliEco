"use client";

import { memo, useState, useCallback, useMemo, useEffect, useRef } from "react";
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  useSensor,
  useSensors,
  useDroppable,
  pointerWithin,
  closestCenter,
  type CollisionDetection,
  type DragStartEvent,
  type DragEndEvent,
  type DragOverEvent,
} from "@dnd-kit/core";
import { SortableContext, useSortable, verticalListSortingStrategy, arrayMove } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Virtuoso } from "react-virtuoso";
import { ChevronRight, EyeOff, GripVertical, MoreHorizontal, Pencil, Plus } from "lucide-react";
import { useQuery, useQueries, useQueryClient } from "@tanstack/react-query";
import type {
  Task,
  TaskAssigneeType,
  TaskStatus,
  TaskTableGroupDescriptor,
  Project,
  UpdateTaskRequest,
} from "@silieco/core/types";
import { useViewStore, useViewStoreApi } from "@silieco/core/tasks/stores/view-store-context";
import { filterTasks, type TaskFilters } from "../utils/filter";
import { getMoveAnchors } from "../utils/drag-utils";
import type { SwimlaneGrouping } from "@silieco/core/tasks/stores/view-store";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useActorName } from "@silieco/core/workspace/hooks";
import { useLoadMoreByStatus } from "@silieco/core/tasks/mutations";
import { childrenByParentsOptions, taskKeys, type TaskSortParam, type MyTasksFilter } from "@silieco/core/tasks/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@silieco/ui/components/ui/dropdown-menu";
import { sortTasks } from "../utils/sort";
import { ALL_STATUSES, STATUS_CONFIG } from "@silieco/core/tasks/config";
import { DraggableBoardCard, BoardCardContent } from "./board-card";
import { StatusIcon } from "./status-icon";
import { Button } from "@silieco/ui/components/ui/button";
import { StatusHeading } from "./status-heading";
import { HiddenColumnsPanel, HiddenColumnRow } from "./hidden-columns-panel";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";
import { ListLoadMoreFooter } from "./list-load-more-footer";
import { AppLink } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { VirtuosoSeed } from "../../common/virtuoso-seed";

import { DeferredPopup } from "../../common/deferred-popup";
import { useRestoredScrollOffset, useRestoredScrollRef } from "../../platform";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import type { ChildProgress } from "./list-row";
import { useT } from "../../i18n";
import type { TaskCreateDefaults } from "../surface/types";
import type {
  TaskGroupBranches,
  TaskGroupPageState,
} from "../surface/use-task-group-branches";

const COLUMN_WIDTH = 280;
const COLUMN_GAP = 16;

// A swimlane row (header + one row of card cells) is ~300px+ tall — a
// viewport fits ~3. The generic VIRTUOSO_SEED_COUNT (30, sized for 36px list
// rows) made every surface remount synchronously mount up to 30 full lanes
// (each lane = statuses x cells x cards); 6 covers the viewport with margin.
const SWIMLANE_LANE_SEED_COUNT = 6;

// Hoisted out of SwimLaneView so its reference is stable across renders —
// useQueries' combine option uses it through replaceEqualDeep, but keeping
// the function stable saves the per-render reference check.
function combineChildrenLists(
  results: { data: Task[] | undefined }[],
): (Task[] | undefined)[] {
  return results.map((r) => r.data);
}

type SwimLaneMoveTargetUpdates = Pick<
  UpdateTaskRequest,
  | "parent_task_id"
  | "project_id"
  | "assignee_type"
  | "assignee_id"
  | "status"
  | "position"
>;

type SwimLaneMoveUpdates = SwimLaneMoveTargetUpdates & {
  before_id: string | null;
  after_id: string | null;
};

function makeSwimLaneCollision(cellIds: Set<string>): CollisionDetection {
  return (args) => {
    const activeId = args.active.id as string;
    const isLaneDrag = activeId.startsWith("lane:");

    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      let filtered = pointer;
      if (isLaneDrag) {
        // Lane dragging: only consider other lane headers
        filtered = pointer.filter((c) => (c.id as string).startsWith("lane:"));
      } else {
        // Card dragging: ignore parent lane headers entirely
        filtered = pointer.filter((c) => !(c.id as string).startsWith("lane:"));
      }

      if (filtered.length > 0) {
        const cards = filtered.filter((c) => !cellIds.has(c.id as string));
        if (cards.length > 0) return cards;
        return filtered;
      }
    }

    const closest = closestCenter(args);
    let filteredClosest = closest;
    if (isLaneDrag) {
      filteredClosest = closest.filter((c) => (c.id as string).startsWith("lane:"));
    } else {
      filteredClosest = closest.filter((c) => !(c.id as string).startsWith("lane:"));
    }

    return filteredClosest;
  };
}

function parseCellId(id: string): { laneKey: string; status: string } | null {
  if (!id.startsWith("swim:")) return null;
  const rest = id.slice(5);
  const lastColon = rest.lastIndexOf(":");
  if (lastColon === -1) return null;
  return {
    laneKey: rest.slice(0, lastColon),
    status: rest.slice(lastColon + 1),
  };
}

function findCellIn(
  data: Record<string, Record<string, string[]>>,
  cellIds: Set<string>,
  id: string,
): { laneKey: string; status: string } | null {
  if (cellIds.has(id)) return parseCellId(id);
  for (const [pk, statusMap] of Object.entries(data)) {
    for (const [status, ids] of Object.entries(statusMap)) {
      if (ids.includes(id)) return { laneKey: pk, status };
    }
  }
  return null;
}

function cellId(laneKey: string, status: TaskStatus): string {
  return `swim:${laneKey}:${status}`;
}

const LANE_ID_PREFIX = "lane:";

/** Sentinel id slice (after the grouping prefix) for the pinned no-X lane. */
const NONE_LANE_ID = "none";

/** Sentinel id slice for the parent-grouping orphan fallback lane. */
const ORPHAN_LANE_ID = "__orphans__";

/**
 * Sortable id for a draggable swimlane header. Pinned lanes (no-X) and the
 * orphan fallback get a stable but unique-per-grouping id so dnd-kit doesn't
 * silently collapse them onto a real lane id when both happen to be empty.
 */
function laneIdFor(grouping: SwimlaneGrouping, rawId: string): string {
  return `${LANE_ID_PREFIX}${grouping}:${rawId}`;
}

function parseLaneId(id: string): { grouping: string; rawId: string } | null {
  if (!id.startsWith(LANE_ID_PREFIX)) return null;
  const rest = id.slice(LANE_ID_PREFIX.length);
  const firstColon = rest.indexOf(":");
  if (firstColon === -1) return null;
  return {
    grouping: rest.slice(0, firstColon),
    rawId: rest.slice(firstColon + 1),
  };
}

function computePosition(ids: string[], activeId: string, taskMap: Map<string, Task>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => taskMap.get(id)?.position ?? 0;
  if (ids.length === 1) return taskMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

/**
 * One swimlane row. Lanes are produced by a per-grouping builder
 * (`buildParentLanes` / `buildProjectLanes` / `buildAssigneeLanes`) and then
 * consumed uniformly by the renderer. The matcher/move-updates closures hide
 * the grouping-specific details behind a single interface so the drag-end
 * handler doesn't need to branch on grouping.
 */
interface LaneGroup {
  /** Full lane key — `<grouping>:<rawId>`. Used in cell ids and as the dictionary key. */
  key: string;
  /** Unique-within-grouping id slice (after the grouping prefix). */
  rawId: string;
  /** Pinned lane (no-X). Always rendered at the top, never draggable. */
  isPinned: boolean;
  /**
   * Display-only fallback bucket for the parent grouping — children whose
   * canonical parent isn't in the loaded set. Drops in/out are rejected
   * because we can't synthesize the missing parent id.
   */
  isOrphan: boolean;
  title: string;
  identifier: string;
  /** Parent task (parent grouping only) — drives the open-parent link + status icon in the header. */
  parentTask: Pick<Task, "id" | "status"> | null;
  /** Project metadata (project grouping only) — drives the icon in the header. */
  project: Project | null;
  /** Actor (assignee grouping only) — drives the avatar in the header. */
  actor: { type: TaskAssigneeType; id: string } | null;
  /** Whether this lane owns `task`. */
  matches: (task: Task) => boolean;
  /**
   * Payload fragment emitted to `onMoveTask` when an task is dropped into
   * this lane. Status + position are filled in by the drag-end handler.
   */
  moveUpdates: SwimLaneMoveTargetUpdates;
  /** Exact server count; legacy builders leave this undefined and use the
   * loaded cell window as before. */
  total?: number;
  /** Opaque compound row keys by status. */
  serverCellKeys?: Partial<Record<TaskStatus, string>>;
}

const EMPTY_PROGRESS_MAP = new Map<string, ChildProgress>();
// Stable reference for non-parent groupings — keeps the `statusTotals` /
// `cells` memos from busting on every render when there are no headers.
const EMPTY_HEADER_IDS = new Set<string>();
const EMPTY_PROJECTS: Project[] = [];

/**
 * Build parent-grouping lanes. The "No parent" lane is always pinned at the
 * top; the "Other parents" fallback (children whose parent isn't loaded) is
 * appended after it when present.
 */
function buildParentLanes(
  visibleTasks: Task[],
  metadataTasks: Task[],
  storedOrder: string[],
  labels: { noParent: string; otherParents: string },
): LaneGroup[] {
  const metadataMap = new Map<string, Task>();
  for (const task of metadataTasks) metadataMap.set(task.id, task);

  const seen = new Map<string, LaneGroup>();
  let hasOrphan = false;
  for (const task of visibleTasks) {
    if (task.parent_task_id === null) continue;
    const parent = metadataMap.get(task.parent_task_id);
    if (!parent) {
      hasOrphan = true;
      continue;
    }
    const key = `parent:${task.parent_task_id}`;
    if (!seen.has(key)) {
      const parentId = task.parent_task_id;
      seen.set(key, {
        key,
        rawId: parentId,
        isPinned: false,
        isOrphan: false,
        title: parent.title,
        identifier: parent.identifier,
        parentTask: parent,
        project: null,
        actor: null,
        matches: (i) => i.parent_task_id === parentId,
        moveUpdates: { parent_task_id: parentId },
      });
    }
  }

  const orderIndex = new Map<string, number>();
  storedOrder.forEach((parentId, idx) => orderIndex.set(`parent:${parentId}`, idx));
  const ordered = Array.from(seen.values()).sort((a, b) => {
    const ai = orderIndex.get(a.key);
    const bi = orderIndex.get(b.key);
    if (ai !== undefined && bi !== undefined) return ai - bi;
    if (ai !== undefined) return -1;
    if (bi !== undefined) return 1;
    return 0;
  });

  const lanes: LaneGroup[] = [
    {
      key: `parent:${NONE_LANE_ID}`,
      rawId: NONE_LANE_ID,
      isPinned: true,
      isOrphan: false,
      title: labels.noParent,
      identifier: "",
      parentTask: null,
      project: null,
      actor: null,
      matches: (i) => i.parent_task_id === null,
      moveUpdates: { parent_task_id: null },
    },
  ];
  if (hasOrphan) {
    lanes.push({
      key: `parent:${ORPHAN_LANE_ID}`,
      rawId: ORPHAN_LANE_ID,
      isPinned: true,
      isOrphan: true,
      title: labels.otherParents,
      identifier: "",
      parentTask: null,
      project: null,
      actor: null,
      // Match the canonical filter logic: a child whose parent isn't a
      // header lane in the current render.
      matches: () => false,
      moveUpdates: {},
    });
  }
  lanes.push(...ordered);
  return lanes;
}

function buildProjectLanes(
  visibleTasks: Task[],
  projects: Project[],
  storedOrder: string[],
  labels: { noProject: string },
): LaneGroup[] {
  const projectMap = new Map<string, Project>();
  for (const p of projects) projectMap.set(p.id, p);

  const seen = new Map<string, LaneGroup>();
  for (const task of visibleTasks) {
    if (task.project_id === null) continue;
    const key = `project:${task.project_id}`;
    if (seen.has(key)) continue;
    const project = projectMap.get(task.project_id) ?? null;
    const projectId = task.project_id;
    seen.set(key, {
      key,
      rawId: projectId,
      isPinned: false,
      isOrphan: false,
      title: project?.title ?? "",
      identifier: "",
      parentTask: null,
      project,
      actor: null,
      matches: (i) => i.project_id === projectId,
      moveUpdates: { project_id: projectId },
    });
  }

  const orderIndex = new Map<string, number>();
  storedOrder.forEach((id, idx) => orderIndex.set(`project:${id}`, idx));
  const ordered = Array.from(seen.values()).sort((a, b) => {
    const ai = orderIndex.get(a.key);
    const bi = orderIndex.get(b.key);
    if (ai !== undefined && bi !== undefined) return ai - bi;
    if (ai !== undefined) return -1;
    if (bi !== undefined) return 1;
    return a.title.localeCompare(b.title);
  });

  return [
    {
      key: `project:${NONE_LANE_ID}`,
      rawId: NONE_LANE_ID,
      isPinned: true,
      isOrphan: false,
      title: labels.noProject,
      identifier: "",
      parentTask: null,
      project: null,
      actor: null,
      matches: (i) => i.project_id === null,
      moveUpdates: { project_id: null },
    },
    ...ordered,
  ];
}

function buildAssigneeLanes(
  visibleTasks: Task[],
  getActorName: (type: string, id: string) => string,
  storedOrder: string[],
  labels: { noAssignee: string },
): LaneGroup[] {
  const seen = new Map<string, LaneGroup>();
  for (const task of visibleTasks) {
    if (task.assignee_type === null || task.assignee_id === null) continue;
    const assigneeType: TaskAssigneeType = task.assignee_type;
    const assigneeId = task.assignee_id;
    const rawId = `${assigneeType}:${assigneeId}`;
    const key = `assignee:${rawId}`;
    if (seen.has(key)) continue;
    seen.set(key, {
      key,
      rawId,
      isPinned: false,
      isOrphan: false,
      title: getActorName(assigneeType, assigneeId),
      identifier: "",
      parentTask: null,
      project: null,
      actor: { type: assigneeType, id: assigneeId },
      matches: (i) =>
        i.assignee_type === assigneeType && i.assignee_id === assigneeId,
      moveUpdates: {
        assignee_type: assigneeType,
        assignee_id: assigneeId,
      },
    });
  }

  // Sort by actor type (members before agents before squads) then by name.
  const typeOrder: Record<string, number> = { member: 0, agent: 1, squad: 2 };
  const orderIndex = new Map<string, number>();
  storedOrder.forEach((id, idx) => orderIndex.set(`assignee:${id}`, idx));
  const ordered = Array.from(seen.values()).sort((a, b) => {
    const ai = orderIndex.get(a.key);
    const bi = orderIndex.get(b.key);
    if (ai !== undefined && bi !== undefined) return ai - bi;
    if (ai !== undefined) return -1;
    if (bi !== undefined) return 1;
    const at = typeOrder[a.actor?.type ?? ""] ?? 99;
    const bt = typeOrder[b.actor?.type ?? ""] ?? 99;
    if (at !== bt) return at - bt;
    return a.title.localeCompare(b.title);
  });

  return [
    {
      key: `assignee:${NONE_LANE_ID}`,
      rawId: NONE_LANE_ID,
      isPinned: true,
      isOrphan: false,
      title: labels.noAssignee,
      identifier: "",
      parentTask: null,
      project: null,
      actor: null,
      matches: (i) => i.assignee_id === null,
      moveUpdates: { assignee_type: null, assignee_id: null },
    },
    ...ordered,
  ];
}

function buildServerLanes(
  descriptors: readonly TaskTableGroupDescriptor[],
  grouping: SwimlaneGrouping,
  visibleStatuses: readonly TaskStatus[],
  projects: ReadonlyMap<string, Project> | undefined,
  getActorName: (type: string, id: string) => string,
  storedOrder: string[],
  labels: {
    noParent: string;
    otherParents: string;
    noProject: string;
    noAssignee: string;
  },
): LaneGroup[] {
  const visibleStatusSet = new Set(visibleStatuses);
  const lanes = descriptors.flatMap((descriptor): LaneGroup[] => {
    if (
      (descriptor.secondary_groups ?? []).every(
        (secondary) =>
          secondary.value.kind !== "status" ||
          !visibleStatusSet.has(secondary.value.status as TaskStatus) ||
          secondary.count === 0,
      )
    ) {
      return [];
    }
    const serverCellKeys = Object.fromEntries(
      (descriptor.secondary_groups ?? []).flatMap((secondary) =>
        secondary.value.kind === "status"
          ? [[secondary.value.status, secondary.key]]
          : [],
      ),
    ) as Partial<Record<TaskStatus, string>>;
    const value = descriptor.value;
    if (grouping === "assignee" && value.kind === "assignee") {
      const actorRef = value.actor;
      const actor: { type: TaskAssigneeType; id: string } | null =
        actorRef &&
        (actorRef.type === "member" ||
          actorRef.type === "agent" ||
          actorRef.type === "squad")
          ? { type: actorRef.type, id: actorRef.id }
          : null;
      const rawId = actor ? `${actor.type}:${actor.id}` : NONE_LANE_ID;
      return [{
        key: `assignee:${rawId}`,
        rawId,
        isPinned: actor === null,
        isOrphan: false,
        title: actor
          ? getActorName(actor.type, actor.id)
          : labels.noAssignee,
        identifier: "",
        parentTask: null,
        project: null,
        actor,
        matches: (task) =>
          actor
            ? task.assignee_type === actor.type &&
              task.assignee_id === actor.id
            : task.assignee_type === null && task.assignee_id === null,
        moveUpdates: actor
          ? { assignee_type: actor.type, assignee_id: actor.id }
          : { assignee_type: null, assignee_id: null },
        total: descriptor.count,
        serverCellKeys,
      }];
    }
    if (grouping === "project" && value.kind === "project") {
      const rawId = value.project_id ?? NONE_LANE_ID;
      const project = value.project_id
        ? projects?.get(value.project_id) ?? null
        : null;
      return [{
        key: `project:${rawId}`,
        rawId,
        isPinned: value.project_id === null,
        isOrphan: false,
        title: value.project_id ? project?.title ?? "" : labels.noProject,
        identifier: "",
        parentTask: null,
        project,
        actor: null,
        matches: (task) => task.project_id === value.project_id,
        moveUpdates: { project_id: value.project_id },
        total: descriptor.count,
        serverCellKeys,
      }];
    }
    if (grouping === "parent" && value.kind === "parent") {
      const rawId = value.parent_id ?? NONE_LANE_ID;
      const unavailable = value.value_state === "unavailable";
      return [{
        key: `parent:${rawId}`,
        rawId,
        isPinned: value.parent_id === null || unavailable,
        isOrphan: unavailable,
        title:
          value.parent?.title ??
          (unavailable ? labels.otherParents : labels.noParent),
        identifier: value.parent?.identifier ?? "",
        parentTask: value.parent
          ? { id: value.parent.id, status: value.parent.status as TaskStatus }
          : null,
        project: null,
        actor: null,
        matches: (task) => task.parent_task_id === value.parent_id,
        moveUpdates: unavailable
          ? {}
          : { parent_task_id: value.parent_id },
        total: descriptor.count,
        serverCellKeys,
      }];
    }
    return [];
  });
  const orderIndex = new Map<string, number>();
  storedOrder.forEach((rawId, index) => orderIndex.set(rawId, index));
  const originalIndex = new Map(
    lanes.map((lane, index) => [lane.rawId, index]),
  );
  return lanes.toSorted((a, b) => {
    if (a.isPinned !== b.isPinned) return a.isPinned ? -1 : 1;
    const ai = orderIndex.get(a.rawId);
    const bi = orderIndex.get(b.rawId);
    if (ai !== undefined && bi !== undefined) return ai - bi;
    if (ai !== undefined) return -1;
    if (bi !== undefined) return 1;
    return (originalIndex.get(a.rawId) ?? 0) -
      (originalIndex.get(b.rawId) ?? 0);
  });
}

function SwimLaneViewImpl({
  tasks,
  unfilteredTasks,
  activeFilters: activeFiltersProp,
  visibleStatuses = ALL_STATUSES,
  hiddenStatuses = [],
  onMoveTask,
  childProgressMap = EMPTY_PROGRESS_MAP,
  projectMap,
  myTasksScope,
  myTasksFilter,
  sort,
  projectId,
  onCreateTask,
  groupBranches,
}: {
  tasks: Task[];
  /**
   * Status-unfiltered companion set used for parent metadata lookup and
   * status totals. Lane discovery still drives off the visible `tasks`
   * set so that parents whose children are all in hidden statuses don't
   * produce empty rows, but header chrome (identifier, title, task ref
   * for the Open-parent link) and hidden-column counts read from here so
   * a parent in a hidden status still surfaces its label correctly.
   */
  unfilteredTasks?: Task[];
  activeFilters?: Omit<TaskFilters, "statusFilters">;
  visibleStatuses?: TaskStatus[];
  hiddenStatuses?: TaskStatus[];
  onMoveTask: (
    taskId: string,
    updates: SwimLaneMoveUpdates,
    onSettled?: () => void,
  ) => void;
  childProgressMap?: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  myTasksScope?: string;
  myTasksFilter?: MyTasksFilter;
  /** Must match the sort the page queried with — embedded in the cache key. */
  sort?: TaskSortParam;
  /** Pre-fills `project_id` on the create form for the in-cell "+" button. */
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  groupBranches?: TaskGroupBranches;
}) {
  const { t } = useT("tasks");
  const paths = useWorkspacePaths();
  const viewStoreApi = useViewStoreApi();
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const swimlaneOrders = useViewStore((s) => s.swimlaneOrders);
  const swimlaneOrder = swimlaneOrders[swimlaneGrouping];

  const wsId = useWorkspaceId();

  const activeFilters = useMemo(() => ({
    // Status is enforced by visible-column rendering, not by filterTasks
    statusFilters: [],
    priorityFilters: activeFiltersProp?.priorityFilters ?? [],
    assigneeFilters: activeFiltersProp?.assigneeFilters ?? [],
    includeNoAssignee: activeFiltersProp?.includeNoAssignee ?? false,
    assigneeFilterActive: activeFiltersProp?.assigneeFilterActive ?? false,
    // Extra children are not part of the server-grouped page yet, so apply
    // the same running-task task ids returned by `/api/working-agents`.
    agentRunningFilter: activeFiltersProp?.agentRunningFilter ?? false,
    runningTaskIds: activeFiltersProp?.runningTaskIds,
    creatorFilters: activeFiltersProp?.creatorFilters ?? [],
    projectFilters: activeFiltersProp?.projectFilters ?? [],
    includeNoProject: activeFiltersProp?.includeNoProject ?? false,
    labelFilters: activeFiltersProp?.labelFilters ?? [],
    // Carry the "Show sub-tasks" toggle through to the extra-children merge
    // path (see `filterTasks(extra, activeFilters)` below); otherwise batch /
    // per-parent loaded sub-tasks get re-added even when the toggle is off.
    showSubTasks: activeFiltersProp?.showSubTasks ?? true,
  }), [activeFiltersProp]);
  const projects = useMemo(
    () =>
      swimlaneGrouping === "project" && projectMap
        ? Array.from(projectMap.values())
        : EMPTY_PROJECTS,
    [projectMap, swimlaneGrouping],
  );
  const { getActorName } = useActorName();

  const laneSourceTasks = unfilteredTasks ?? tasks;

  const myTasksOpts = useMemo(
    () =>
      myTasksScope
        ? { scope: myTasksScope, filter: myTasksFilter ?? {} }
        : undefined,
    [myTasksScope, myTasksFilter],
  );

  // Re-impose canonical status order (ALL_STATUSES) on whatever the controller
  // marked visible, so columns — including `cancelled`, ordered last — render
  // in lifecycle order.
  const sortedStatuses = useMemo(
    () => ALL_STATUSES.filter((s) => visibleStatuses.includes(s)),
    [visibleStatuses],
  );

  const laneLabels = useMemo(
    () => ({
      noParent: t(($) => $.swimlane.no_parent),
      otherParents: t(($) => $.swimlane.other_parents),
      noProject: t(($) => $.swimlane.no_project),
      noAssignee: t(($) => $.swimlane.no_assignee),
    }),
    [t],
  );

  // Candidate parents for the batch fetch (parent grouping only). Union of:
  //   - parent_task_id of visible tasks (child loaded, parent past page)
  //   - id of visible tasks with children per childProgressMap (parent
  //     loaded, grandchild past page — the bug this PR fixes)
  const qc = useQueryClient();
  const batchParentIds = useMemo(() => {
    if (groupBranches?.enabled || swimlaneGrouping !== "parent") return [];
    const ids = new Set<string>();
    const consider = (id: string | null | undefined) => {
      if (!id) return;
      if (qc.getQueryData(taskKeys.children(wsId, id)) === undefined) {
        ids.add(id);
      }
    };
    for (const task of tasks) {
      consider(task.parent_task_id);
      const progress = childProgressMap.get(task.id);
      if (progress && progress.total > 0) consider(task.id);
    }
    return Array.from(ids).sort();
  }, [groupBranches?.enabled, swimlaneGrouping, tasks, childProgressMap]); // eslint-disable-line react-hooks/exhaustive-deps

  const { data: batchChildrenMap } = useQuery(
    childrenByParentsOptions(wsId, batchParentIds, qc),
  );

  // Grows monotonically so lanes don't lose children when the batch key
  // changes — once a parent's cache is hydrated it stays observed. Reset
  // when grouping leaves "parent" so a long session that toggles groupings
  // doesn't accumulate dead subscriptions.
  const subscribedRef = useRef<Set<string>>(new Set());
  const sortedSubscribedRef = useRef<string[]>([]);
  const subscribedParentIds = useMemo(() => {
    if (groupBranches?.enabled || swimlaneGrouping !== "parent") {
      if (subscribedRef.current.size > 0) {
        subscribedRef.current = new Set();
        sortedSubscribedRef.current = [];
      }
      return sortedSubscribedRef.current;
    }
    let changed = false;
    const add = (id: string | null | undefined) => {
      if (!id || subscribedRef.current.has(id)) return;
      subscribedRef.current.add(id);
      changed = true;
    };
    for (const task of tasks) add(task.parent_task_id);
    if (batchChildrenMap) for (const id of batchChildrenMap.keys()) add(id);
    if (changed) sortedSubscribedRef.current = Array.from(subscribedRef.current).sort();
    return sortedSubscribedRef.current;
  }, [groupBranches?.enabled, swimlaneGrouping, tasks, batchChildrenMap]);

  // Pure cache observers — enabled:false so no fetch fires, just re-renders
  // when setQueryData writes to these keys (from batch hydration, optimistic
  // mutations, or WS events).
  const perParentChildrenLists = useQueries({
    queries: subscribedParentIds.map((parentId) => ({
      queryKey: taskKeys.children(wsId, parentId),
      queryFn: async (): Promise<Task[]> => [],
      enabled: false,
    })),
    combine: combineChildrenLists,
  });

  // Merge paginated tasks with batch-fetched children so parent lanes are
  // populated even when children are beyond the first page.
  const mergedTasks = useMemo(() => {
    if (groupBranches?.enabled) return tasks;
    if (swimlaneGrouping !== "parent") return tasks;
    const existingIds = new Set(tasks.map((i) => i.id));
    const extra: Task[] = [];
    const covered = new Set<string>();
    for (let i = 0; i < subscribedParentIds.length; i++) {
      const data = perParentChildrenLists[i];
      if (!data) continue;
      covered.add(subscribedParentIds[i]!);
      for (const child of data) {
        if (!existingIds.has(child.id)) {
          existingIds.add(child.id);
          extra.push(child);
        }
      }
    }
    if (batchChildrenMap) {
      for (const [parentId, children] of batchChildrenMap) {
        if (covered.has(parentId)) continue;
        for (const child of children) {
          if (!existingIds.has(child.id)) {
            existingIds.add(child.id);
            extra.push(child);
          }
        }
      }
    }
    const filteredExtra = filterTasks(extra, activeFilters);
    return filteredExtra.length === 0 ? tasks : [...tasks, ...filteredExtra];
  }, [groupBranches?.enabled, swimlaneGrouping, tasks, perParentChildrenLists, subscribedParentIds, batchChildrenMap, activeFilters]);

  const laneGroups = useMemo<LaneGroup[]>(() => {
    if (groupBranches?.enabled) {
      return buildServerLanes(
        groupBranches.descriptors,
        swimlaneGrouping,
        sortedStatuses,
        projectMap,
        getActorName,
        swimlaneOrder,
        laneLabels,
      );
    }
    if (swimlaneGrouping === "project") {
      return buildProjectLanes(tasks, projects, swimlaneOrder, laneLabels);
    }
    if (swimlaneGrouping === "assignee") {
      return buildAssigneeLanes(tasks, getActorName, swimlaneOrder, laneLabels);
    }
    // Discovery uses `mergedTasks` so batch-fetched grandchildren can
    // promote their parents to lane headers. Metadata uses `laneSourceTasks`
    // so headers resolve for parents in hidden statuses.
    return buildParentLanes(mergedTasks, laneSourceTasks, swimlaneOrder, laneLabels);
  }, [
    swimlaneGrouping,
    tasks,
    mergedTasks,
    laneSourceTasks,
    projects,
    getActorName,
    swimlaneOrder,
    laneLabels,
    groupBranches,
    projectMap,
    sortedStatuses,
  ]);

  // For parent grouping: tasks that are themselves lane headers should not
  // also appear as cards (that would be a double-render). Other groupings
  // never collide this way (lanes are projects/actors, not tasks), so the
  // set is empty there.
  const headerTaskIds = useMemo(() => {
    if (swimlaneGrouping !== "parent") {
      return EMPTY_HEADER_IDS;
    }
    return new Set(
      laneGroups
        .filter((g) => g.parentTask !== null)
        .map((g) => g.parentTask!.id),
    );
  }, [laneGroups, swimlaneGrouping]);

  // Map of task id → owning lane key. Used by orphan detection for parent
  // grouping (a child whose canonical parent isn't a lane header here lands
  // in the fallback) and as the matcher hot-path for project/assignee.
  const cells = useMemo(() => {
    const result: Record<string, Record<string, string[]>> = {};
    for (const lane of laneGroups) {
      const cellMap: Record<string, string[]> = {};
      for (const status of sortedStatuses) cellMap[status] = [];
      result[lane.key] = cellMap;
    }

    const orphanLane =
      swimlaneGrouping === "parent"
        ? laneGroups.find((g) => g.isOrphan) ?? null
        : null;

    const taskSource = swimlaneGrouping === "parent" ? mergedTasks : tasks;
    const sorted = sortTasks(taskSource, sortBy, sortDirection);
    for (const task of sorted) {
      let placed = false;
      for (const lane of laneGroups) {
        if (lane.isOrphan) continue;
        if (lane.matches(task)) {
          // "No parent" lane only: skip tasks that are themselves lane
          // headers (avoid duplicate card + header). Real-parent lanes keep
          // the dual render so grandparent lanes are never empty.
          if (
            swimlaneGrouping === "parent" &&
            lane.rawId === NONE_LANE_ID &&
            headerTaskIds.has(task.id)
          ) {
            placed = true;
            break;
          }
          const status = task.status;
          if (result[lane.key]?.[status]) {
            result[lane.key]![status]!.push(task.id);
            placed = true;
            break;
          }
        }
      }
      // Parent grouping: a child whose parent isn't a header here falls
      // into the orphan fallback so it doesn't silently disappear.
      if (!placed && orphanLane && task.parent_task_id !== null) {
        const status = task.status;
        if (result[orphanLane.key]?.[status]) {
          result[orphanLane.key]![status]!.push(task.id);
        }
      }
    }
    return result;
  }, [tasks, mergedTasks, laneGroups, sortedStatuses, sortBy, sortDirection, headerTaskIds, swimlaneGrouping]);

  const laneByKey = useMemo(() => {
    const map = new Map<string, LaneGroup>();
    for (const lane of laneGroups) map.set(lane.key, lane);
    return map;
  }, [laneGroups]);

  const cellSet = useMemo(() => {
    const ids = new Set<string>();
    for (const lane of laneGroups) {
      for (const status of sortedStatuses) {
        ids.add(cellId(lane.key, status));
      }
    }
    return ids;
  }, [laneGroups, sortedStatuses]);

  // Drives both visible status-header counts AND hidden-column panel rows.
  // Known limitation (parent grouping): `headerTaskIds` is derived from
  // lanes that exist in the current render (only parents with ≥1 visible
  // child). A parent whose children are all hidden doesn't get a lane, so
  // it counts as a card here. When the user un-hides that status the
  // parent gets promoted to a lane header and the count for that status
  // drops by 1. Tracked as a follow-up.
  const statusTotals = useMemo(() => {
    if (groupBranches?.enabled) {
      const totals = new Map<TaskStatus, number>();
      for (const lane of groupBranches.descriptors) {
        for (const cell of lane.secondary_groups ?? []) {
          if (cell.value.kind !== "status") continue;
          const status = cell.value.status as TaskStatus;
          totals.set(status, (totals.get(status) ?? 0) + cell.count);
        }
      }
      return totals;
    }
    const totals = new Map<TaskStatus, number>();
    for (const task of laneSourceTasks) {
      if (headerTaskIds.has(task.id)) continue;
      totals.set(task.status, (totals.get(task.status) ?? 0) + 1);
    }
    return totals;
  }, [groupBranches, laneSourceTasks, headerTaskIds]);

  // Collapsed swimlanes — persisted per-grouping via the view store. The
  // store keys are raw lane ids (or sentinel `NONE_LANE_ID` / `ORPHAN_LANE_ID`
  // for the pinned lanes), namespaced here as `<grouping>:<rawId>`. Convert
  // on read/write so the store can stay grouping-agnostic.
  const collapsedSwimlanesMap = useViewStore((s) => s.collapsedSwimlanes);
  const collapsedLanes = useMemo(() => {
    const stored = collapsedSwimlanesMap[swimlaneGrouping] ?? [];
    const set = new Set<string>();
    for (const id of stored) set.add(`${swimlaneGrouping}:${id}`);
    return set;
  }, [collapsedSwimlanesMap, swimlaneGrouping]);
  const toggleLane = useCallback(
    (laneKey: string) => {
      const prefix = `${swimlaneGrouping}:`;
      const storeKey = laneKey.startsWith(prefix)
        ? laneKey.slice(prefix.length)
        : laneKey;
      viewStoreApi.getState().toggleSwimlaneCollapsed(storeKey);
    },
    [viewStoreApi, swimlaneGrouping],
  );

  const [activeTask, setActiveTask] = useState<Task | null>(null);
  // The outer scroll box is the customScrollParent for the lane Virtuoso.
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  // Pull-based scroll restoration (SILI-4741): same wiring as board/list/
  // task-detail — ref-attach assigns the saved offset pre-paint, and the
  // lane Virtuoso is born at it via initialScrollTop.
  const restoredScrollTop = useRestoredScrollOffset("swimlane");
  const restoreScrollRef = useRestoredScrollRef("swimlane");
  const attachScroller = useCallback(
    (el: HTMLDivElement | null) => {
      setScrollEl(el);
      restoreScrollRef(el);
    },
    [restoreScrollRef],
  );
  const isDraggingRef = useRef(false);
  // Settle lock: held from drop until the move mutation settles, so a cache
  // change that lands mid-flight (e.g. a membership refetch) does not rebuild
  // localCells out from under the optimistic move. Mirrors board-view /
  // list-view. settleVersion forces the resync once the lock releases.
  const isSettlingRef = useRef(false);
  const [settleVersion, setSettleVersion] = useState(0);

  const taskMap = useMemo(() => {
    const map = new Map<string, Task>();
    for (const task of mergedTasks) map.set(task.id, task);
    return map;
  }, [mergedTasks]);

  const taskMapRef = useRef(taskMap);
  if (!isDraggingRef.current && !isSettlingRef.current) {
    taskMapRef.current = taskMap;
  }

  const [localCells, setLocalCells] = useState(cells);
  const localCellsRef = useRef(localCells);
  localCellsRef.current = localCells;

  useEffect(() => {
    if (!isDraggingRef.current && !isSettlingRef.current) {
      setLocalCells(cells);
    }
  }, [cells, settleVersion]);

  const recentlyMovedRef = useRef(false);
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      recentlyMovedRef.current = false;
    });
    return () => cancelAnimationFrame(id);
  }, [localCells]);

  const collisionDetection = useMemo(
    () => makeSwimLaneCollision(cellSet),
    [cellSet],
  );

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    isDraggingRef.current = true;
    const activeId = event.active.id as string;
    // Lane drags don't carry an Task payload — clear the card overlay so
    // we don't show a stale card during a lane reorder.
    if (parseLaneId(activeId) !== null) {
      setActiveTask(null);
      return;
    }
    const task = taskMapRef.current.get(activeId) ?? null;
    setActiveTask(task);
  }, []);

  const handleDragOver = useCallback(
    (event: DragOverEvent) => {
      const { active, over } = event;
      if (!over || recentlyMovedRef.current) return;

      const activeId = active.id as string;
      const overId = over.id as string;

      setLocalCells((prev) => {
        const activeCell = findCellIn(prev, cellSet, activeId);
        const overCell = findCellIn(prev, cellSet, overId);
        if (!activeCell || !overCell) return prev;
        if (
          activeCell.laneKey === overCell.laneKey &&
          activeCell.status === overCell.status
        ) {
          return prev;
        }
        // The "Other parents" lane is display-only: never let a card
        // enter or leave it via drag. The lane represents children whose
        // canonical parent isn't loaded, so any cross-lane move would
        // either lose that parent (when leaving) or invent a new one
        // (when entering).
        if (
          laneByKey.get(activeCell.laneKey)?.isOrphan ||
          laneByKey.get(overCell.laneKey)?.isOrphan
        ) {
          return prev;
        }

        // Self-parent guard — see handleDragEnd for rationale.
        const overLane = laneByKey.get(overCell.laneKey);
        if (
          overLane &&
          overLane.parentTask !== null &&
          overLane.parentTask.id === activeId
        ) {
          return prev;
        }

        recentlyMovedRef.current = true;

        if (activeCell.laneKey === overCell.laneKey) {
          // Same lane row, different status column
          const row = prev[activeCell.laneKey] ?? {};
          const sourceIds = (row[activeCell.status] ?? []).filter((id) => id !== activeId);
          const targetIds = (row[overCell.status] ?? []).filter((id) => id !== activeId);

          const overIndex = targetIds.indexOf(overId);
          const insertIndex = overIndex >= 0 ? overIndex : targetIds.length;
          targetIds.splice(insertIndex, 0, activeId);

          return {
            ...prev,
            [activeCell.laneKey]: {
              ...row,
              [activeCell.status]: sourceIds,
              [overCell.status]: targetIds,
            },
          };
        }

        // Different lane rows
        const sourceRow = prev[activeCell.laneKey] ?? {};
        const targetRow = prev[overCell.laneKey] ?? {};

        const sourceIds = (sourceRow[activeCell.status] ?? []).filter((id) => id !== activeId);
        const targetIds = (targetRow[overCell.status] ?? []).filter((id) => id !== activeId);

        const overIndex = targetIds.indexOf(overId);
        const insertIndex = overIndex >= 0 ? overIndex : targetIds.length;
        targetIds.splice(insertIndex, 0, activeId);

        return {
          ...prev,
          [activeCell.laneKey]: {
            ...sourceRow,
            [activeCell.status]: sourceIds,
          },
          [overCell.laneKey]: {
            ...targetRow,
            [overCell.status]: targetIds,
          },
        };
      });
    },
    [cellSet, laneByKey],
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      isDraggingRef.current = false;
      setActiveTask(null);

      const reset = () => setLocalCells(cells);

      if (!over) {
        reset();
        return;
      }

      const activeId = active.id as string;
      const overId = over.id as string;

      // Lane reorder runs before the card-move logic because lane ids
      // don't resolve to any cell.
      const activeLaneRef = parseLaneId(activeId);
      const overLaneRef = parseLaneId(overId);
      if (
        activeLaneRef &&
        overLaneRef &&
        activeLaneRef.rawId !== overLaneRef.rawId
      ) {
        // Visible non-pinned lanes, in current render order.
        const visibleOrder = laneGroups
          .filter((g) => !g.isPinned && !g.isOrphan)
          .map((g) => g.rawId);
        const fromIdx = visibleOrder.indexOf(activeLaneRef.rawId);
        const toIdx = visibleOrder.indexOf(overLaneRef.rawId);
        if (fromIdx === -1 || toIdx === -1 || fromIdx === toIdx) return;
        const visibleNext = arrayMove(visibleOrder, fromIdx, toIdx);

        // Merge into the persisted order without clobbering entries that
        // aren't currently visible (e.g. hidden by a status filter).
        // Walk stored, overwriting each visible slot with the next id
        // from `visibleNext`; non-visible entries pass through verbatim.
        // Any remaining `visibleNext` ids (visible lanes that weren't
        // in stored at all) get appended at the end.
        const stored =
          viewStoreApi.getState().swimlaneOrders[swimlaneGrouping] ?? [];
        const visibleSet = new Set(visibleOrder);
        let cursor = 0;
        const merged = stored.map((id) =>
          visibleSet.has(id) ? visibleNext[cursor++]! : id,
        );
        for (const id of visibleNext.slice(cursor)) merged.push(id);

        viewStoreApi.getState().setSwimlaneOrder(merged);
        return;
      }
      if (activeLaneRef || overLaneRef) return;

      const cols = localCellsRef.current;

      const activeCell = findCellIn(cols, cellSet, activeId);
      const overCell = findCellIn(cols, cellSet, overId);
      if (!activeCell || !overCell) {
        reset();
        return;
      }

      // The "Other parents" lane is display-only. Refuse any drop where
      // either the source or the original target cell belongs to it —
      // no re-parenting (we don't know the canonical parent), no
      // position write (siblings here belong to different parents).
      if (
        laneByKey.get(activeCell.laneKey)?.isOrphan ||
        laneByKey.get(overCell.laneKey)?.isOrphan
      ) {
        reset();
        return;
      }

      // Self-parent guard (parent grouping only): refuse drops where the
      // dragged card is the same task as the target lane's parent. The
      // backend rejects this as a self-cycle; intercepting client-side
      // avoids the optimistic-move → rollback flicker.
      const targetLaneForGuard = laneByKey.get(overCell.laneKey);
      if (
        targetLaneForGuard &&
        targetLaneForGuard.parentTask !== null &&
        targetLaneForGuard.parentTask.id === activeId
      ) {
        reset();
        return;
      }

      let finalCells = cols;
      // Handle reordering within the same target cell upon drop.
      if (
        activeCell.laneKey === overCell.laneKey &&
        activeCell.status === overCell.status
      ) {
        const ids = cols[activeCell.laneKey]?.[activeCell.status];
        if (ids) {
          const oldIndex = ids.indexOf(activeId);
          const newIndex = ids.indexOf(overId);
          if (oldIndex !== -1 && newIndex !== -1 && oldIndex !== newIndex) {
            const reordered = arrayMove(ids, oldIndex, newIndex);
            finalCells = {
              ...cols,
              [activeCell.laneKey]: {
                ...cols[activeCell.laneKey],
                [activeCell.status]: reordered,
              },
            };
            setLocalCells(finalCells);
          }
        }
      }

      const finalOverCell = findCellIn(finalCells, cellSet, activeId);
      if (!finalOverCell) {
        reset();
        return;
      }

      const finalIds = finalCells[finalOverCell.laneKey]?.[finalOverCell.status] ?? [];
      const newPosition = computePosition(finalIds, activeId, taskMapRef.current);
      const currentTask = taskMapRef.current.get(activeId);
      const targetLane = laneByKey.get(finalOverCell.laneKey);
      if (!targetLane) {
        reset();
        return;
      }

      if (
        currentTask &&
        targetLane.matches(currentTask) &&
        currentTask.status === (finalOverCell.status as TaskStatus) &&
        currentTask.position === newPosition
      ) {
        return;
      }

      isSettlingRef.current = true;
      onMoveTask(
        activeId,
        {
          ...targetLane.moveUpdates,
          status: finalOverCell.status as TaskStatus,
          position: newPosition,
          ...getMoveAnchors(finalIds, activeId),
        },
        () => {
          isSettlingRef.current = false;
          setSettleVersion((v) => v + 1);
        },
      );
    },
    [cells, cellSet, laneByKey, laneGroups, onMoveTask, swimlaneGrouping, viewStoreApi],
  );

  // Grid template: one column per status, fixed width COLUMN_WIDTH, gap COLUMN_GAP.
  const trackWidth = sortedStatuses.length * COLUMN_WIDTH + Math.max(0, sortedStatuses.length - 1) * COLUMN_GAP;
  const gridStyle = useMemo(
    () => ({
      display: "grid",
      gridTemplateColumns: `repeat(${sortedStatuses.length}, ${COLUMN_WIDTH}px)`,
      columnGap: `${COLUMN_GAP}px`,
      width: `${trackWidth}px`,
    }) as const,
    [sortedStatuses.length, trackWidth],
  );

  // Lanes render in one Virtuoso so only on-screen lanes stay mounted. Pinned
  // lanes keep their leading position; the SortableContext still wraps the
  // whole set (its `items` are only the non-pinned lane ids, so reorder is
  // unchanged), and per-cell droppables live on always-mounted lane cells.
  const orderedLanes = useMemo(
    () => [
      ...laneGroups.filter((g) => g.isPinned),
      ...laneGroups.filter((g) => !g.isPinned),
    ],
    [laneGroups],
  );
  const nonPinnedLaneIds = useMemo(
    () =>
      laneGroups
        .filter((g) => !g.isPinned)
        .map((g) => laneIdFor(swimlaneGrouping, g.rawId)),
    [laneGroups, swimlaneGrouping],
  );
  // Per-status load-more sentinels ride Virtuoso's Footer so they sit at the
  // true end of the lane list; pt-4 reproduces the previous gap-4.
  const laneComponents = useMemo(
    () => ({
      Footer: () =>
        groupBranches?.enabled ? (
          groupBranches.hasMoreGroups ? (
            <div className="pt-4">
              <InfiniteScrollSentinel
                onVisible={groupBranches.loadMoreGroups}
                loading={groupBranches.isLoadingMoreGroups}
              />
            </div>
          ) : null
        ) : (
          <div className="pt-4">
            <SwimLaneLoadMoreRow
              sortedStatuses={sortedStatuses}
              gridStyle={gridStyle}
              myTasksOpts={myTasksOpts}
              sort={sort}
            />
          </div>
        ),
    }),
    [
      groupBranches?.enabled,
      groupBranches?.hasMoreGroups,
      groupBranches?.isLoadingMoreGroups,
      groupBranches?.loadMoreGroups,
      sortedStatuses,
      gridStyle,
      myTasksOpts,
      sort,
    ],
  );

  const computeLaneKey = (_index: number, lane: LaneGroup) => lane.key;
  const renderLane = (index: number, lane: LaneGroup) => (
    <div className={index === 0 ? undefined : "pt-4"}>
      <DraggableSwimLane
        lane={lane}
        grouping={swimlaneGrouping}
        isCollapsed={collapsedLanes.has(lane.key)}
        onToggleCollapse={() => toggleLane(lane.key)}
        localCells={localCells}
        sortedStatuses={sortedStatuses}
        taskMap={taskMapRef.current}
        childProgressMap={childProgressMap}
        projectMap={projectMap}
        gridStyle={gridStyle}
        paths={paths}
        projectId={projectId}
        onCreateTask={onCreateTask}
        groupPagination={groupBranches?.pagination}
      />
    </div>
  );

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={collisionDetection}
      onDragStart={handleDragStart}
      onDragOver={handleDragOver}
      onDragEnd={handleDragEnd}
    >
      <div ref={attachScroller} data-tab-scroll-root="swimlane" className="flex flex-1 min-h-0 gap-4 overflow-auto p-4">
        <div className="flex shrink-0 flex-col" style={{ width: `${trackWidth}px` }}>
        {groupBranches?.isError && laneGroups.length === 0 && (
          <button
            type="button"
            className="py-8 text-body text-destructive hover:underline"
            onClick={groupBranches.retryGroups}
          >
            {t(($) => $.table.load_more_failed_retry)}
          </button>
        )}
        {/* Sticky status header row — visually matches the top of a BoardColumn */}
        <div className="sticky top-0 z-10 mb-2 bg-background/95 pb-2 backdrop-blur supports-[backdrop-filter]:bg-background/75">
          <div style={gridStyle}>
            {sortedStatuses.map((status) => {
              const cfg = STATUS_CONFIG[status];
              const total = statusTotals.get(status) ?? 0;
              return (
                <div
                  key={status}
                  className={`flex items-center justify-between rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} px-3 py-2`}
                >
                  <StatusHeading status={status} count={total} />
                  {/* Lazy-mounted like the board's column menu — see
                      DeferredPopup. */}
                  <DeferredPopup
                    ariaHasPopup="menu"
                    triggerRender={
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t(($) => $.board.hide_column)}
                        className="rounded-full text-muted-foreground"
                      >
                        <MoreHorizontal className="size-3.5" />
                      </Button>
                    }
                  >
                    {(open, onOpenChange) => (
                      <DropdownMenu open={open} onOpenChange={onOpenChange}>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-sm"
                              aria-label={t(($) => $.board.hide_column)}
                              className="rounded-full text-muted-foreground"
                            >
                              <MoreHorizontal className="size-3.5" />
                            </Button>
                          }
                        />
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onClick={() => viewStoreApi.getState().hideStatus(status)}
                          >
                            <EyeOff className="size-3.5" />
                            {t(($) => $.board.hide_column)}
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                  </DeferredPopup>
                </div>
              );
            })}
          </div>
        </div>

        {/* Lane rows, virtualized. Pinned lanes (the no-X bucket, and
            parent-grouping's orphan fallback) keep their leading position and
            stay non-draggable; the SortableContext still lets the rest reorder
            by dragging the grip handle (its `items` are only the non-pinned
            lane ids). Only on-screen lanes stay mounted. */}
        <SortableContext
          items={nonPinnedLaneIds}
          strategy={verticalListSortingStrategy}
        >
          {/* Seed a bounded slice of real lanes while the scroll ref hasn't
              settled after a remount, so the lane area never paints blank; once
              it's set, mount the Virtuoso with a matching `initialItemCount` to
              survive the measurement frame (SILI-4750). */}
          {scrollEl ? (
            <Virtuoso
              customScrollParent={scrollEl}
              data={orderedLanes}
              computeItemKey={computeLaneKey}
              initialScrollTop={restoredScrollTop}
              initialItemCount={Math.min(orderedLanes.length, SWIMLANE_LANE_SEED_COUNT)}
              increaseViewportBy={{ top: 600, bottom: 600 }}
              components={laneComponents}
              itemContent={renderLane}
            />
          ) : (
            <VirtuosoSeed
              data={orderedLanes}
              itemContent={renderLane}
              computeItemKey={computeLaneKey}
              count={SWIMLANE_LANE_SEED_COUNT}
            />
          )}
        </SortableContext>
        </div>

        {hiddenStatuses.length > 0 && (
          <SwimLaneHiddenColumnsPanel
            hiddenStatuses={hiddenStatuses}
            statusTotals={statusTotals}
          />
        )}
      </div>

      <DragOverlay dropAnimation={null}>
        {activeTask ? (
          <div className="w-[280px] rotate-2 scale-105 cursor-grabbing opacity-90 shadow-lg shadow-black/10">
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

/**
 * Renders a single swimlane (lane header + cells row).
 *
 * Non-pinned lanes are made draggable via `useSortable` so users can reorder
 * them. Pinned lanes (the no-X bucket, and parent-grouping's orphan
 * fallback) pass through with `disabled: true` so they stay pinned and
 * unclickable for drag — `useSortable` must still be called unconditionally
 * to satisfy the rules of hooks.
 *
 * Click vs drag: `PointerSensor` has `activationConstraint: { distance: 5 }`,
 * so taps on the header still toggle collapse while a ≥5px drag starts the
 * sortable interaction. The "Open parent" pencil link stops pointer events
 * so users can click it without inadvertently starting a drag.
 */
function DraggableSwimLane({
  lane,
  grouping,
  isCollapsed,
  onToggleCollapse,
  localCells,
  sortedStatuses,
  taskMap,
  childProgressMap,
  projectMap,
  gridStyle,
  paths,
  projectId,
  onCreateTask,
  groupPagination,
}: {
  lane: LaneGroup;
  grouping: SwimlaneGrouping;
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  localCells: Record<string, Record<string, string[]>>;
  sortedStatuses: TaskStatus[];
  taskMap: Map<string, Task>;
  childProgressMap: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  gridStyle: React.CSSProperties;
  paths: ReturnType<typeof useWorkspacePaths>;
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  groupPagination?: Record<string, TaskGroupPageState>;
}) {
  const { t } = useT("tasks");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: laneIdFor(grouping, lane.rawId),
    disabled: lane.isPinned,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  const laneTotal = sortedStatuses.reduce((sum, status) => {
    const serverKey = lane.serverCellKeys?.[status];
    return (
      sum +
      (serverKey
        ? (groupPagination?.[serverKey]?.total ?? 0)
        : (localCells[lane.key]?.[status]?.length ?? 0))
    );
  }, 0);

  return (
    <div ref={setNodeRef} style={style} className={`flex flex-col ${isDragging ? "opacity-50" : ""}`}>
      {/* Non-interactive container — the inner collapse button and any
          ancillary links (e.g. open-parent) are independent controls so we
          don't nest an <a> inside a <button>. The drag listeners attach
          here so the whole header row is the drag surface. */}
      <div
        className="mb-2 flex w-full items-center gap-2 rounded-md px-1 py-1"
        {...attributes}
        {...listeners}
      >
        {!lane.isPinned && (
          <GripVertical
            className="!size-3 shrink-0 cursor-grab text-muted-foreground/60"
            aria-hidden
          />
        )}
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={t(($) => $.swimlane.toggle_collapse)}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left transition-colors hover:bg-accent/70"
        >
          <ChevronRight
            className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${isCollapsed ? "" : "rotate-90"}`}
          />
          {lane.parentTask && (
            <StatusIcon status={lane.parentTask.status} className="size-3.5" />
          )}
          {lane.project && <ProjectIcon project={lane.project} size="sm" />}
          {lane.actor && (
            <ActorAvatar
              actorType={lane.actor.type}
              actorId={lane.actor.id}
              size="sm"
            />
          )}
          <span className="truncate text-body font-semibold">{lane.title}</span>
          {lane.identifier && (
            <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
              {lane.identifier}
            </span>
          )}
          <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
            {laneTotal}
          </span>
        </button>
        {lane.parentTask && (
          <DeferredTooltip
            content={t(($) => $.swimlane.open_parent)}
            trigger={
              <AppLink
                href={paths.taskDetail(lane.parentTask.id)}
                aria-label={t(($) => $.swimlane.open_parent)}
                className="inline-flex size-5 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <Pencil className="size-3" />
              </AppLink>
            }
          />
        )}
      </div>
      {/* Cells row — each cell mirrors a BoardColumn body */}
      {!isCollapsed && (
        <div style={gridStyle}>
          {sortedStatuses.map((status) => {
            const cId = cellId(lane.key, status);
            const taskIds = localCells[lane.key]?.[status] ?? [];
            return (
              <SwimLaneCell
                key={cId}
                cellId={cId}
                taskIds={taskIds}
                taskMap={taskMap}
                childProgressMap={childProgressMap}
                projectMap={projectMap}
                status={status}
                lane={lane}
                projectId={projectId}
                onCreateTask={onCreateTask}
                readOnly={lane.isOrphan}
                page={
                  lane.serverCellKeys?.[status]
                    ? groupPagination?.[lane.serverCellKeys[status]!]
                    : undefined
                }
              />
            );
          })}
        </div>
      )}
    </div>
  );
}

function SwimLaneCell({
  cellId: cId,
  taskIds,
  taskMap,
  childProgressMap,
  projectMap,
  status,
  lane,
  projectId,
  onCreateTask,
  readOnly = false,
  page,
}: {
  cellId: string;
  taskIds: string[];
  taskMap: Map<string, Task>;
  childProgressMap: Map<string, ChildProgress>;
  projectMap?: Map<string, Project>;
  status: TaskStatus;
  lane: LaneGroup;
  projectId?: string;
  onCreateTask?: (defaults: TaskCreateDefaults) => void;
  /**
   * Display-only cell — the create affordance is suppressed and drag-end
   * upstream refuses to honour drops that would re-anchor a card to this
   * lane. Used by the parent-grouping orphan fallback whose contents
   * belong to parents we don't have loaded.
   */
  readOnly?: boolean;
  page?: TaskGroupPageState;
}) {
  // The orphan cell stays enabled in the collision graph so that drops
  // onto its whitespace area are absorbed here instead of falling through
  // to the nearest real cell. The `isOrphan` guards in handleDragOver /
  // handleDragEnd reject the actual move.
  const { setNodeRef, isOver: droppableIsOver } = useDroppable({ id: cId });
  // Never show the hover highlight on a readOnly cell — the guards will
  // reject the drop, so visual confirmation would be misleading.
  const isOver = readOnly ? false : droppableIsOver;
  const { t } = useT("tasks");
  const cfg = STATUS_CONFIG[status];

  const resolvedTasks = useMemo(
    () =>
      taskIds.flatMap((id) => {
        const task = taskMap.get(id);
        return task ? [task] : [];
      }),
    [taskIds, taskMap],
  );

  const handleAdd = useCallback(() => {
    const data: TaskCreateDefaults = { status, ...lane.moveUpdates };
    // Per-page project override takes precedence (e.g. Project Detail
    // pre-fills its own project id regardless of grouping).
    if (projectId) data.project_id = projectId;
    onCreateTask?.(data);
  }, [status, lane, projectId, onCreateTask]);

  return (
    <div className={`flex min-h-[120px] flex-col rounded-xl ${cfg?.columnBg ?? "bg-muted/40"} p-2`}>
      <div
        ref={setNodeRef}
        className={`flex-1 space-y-2 rounded-lg p-1 transition-colors ${
          isOver ? "bg-accent/60" : ""
        }`}
      >
        <SortableContext items={taskIds} strategy={verticalListSortingStrategy}>
          {resolvedTasks.map((task) => (
            <DraggableBoardCard
              key={task.id}
              task={task}
              childProgress={childProgressMap.get(task.id)}
              project={
                task.project_id ? projectMap?.get(task.project_id) : undefined
              }
            />
          ))}
        </SortableContext>
        {taskIds.length === 0 && (
          <p className="py-6 text-center text-caption text-muted-foreground">
            &mdash;
          </p>
        )}
        {page && (
          <ListLoadMoreFooter
            hasMore={page.hasMore}
            isLoading={page.isLoading || page.isFetching}
            total={page.total}
            onLoadMore={page.loadMore}
            isError={page.isError}
            onRetry={page.retry}
          />
        )}
      </div>
      {/* One of these per lane×status cell (~170 on a real swimlane) —
          eagerly mounted tooltip roots here were the single largest slice
          of swimlane mount cost. */}
      {!readOnly && onCreateTask && (
        <DeferredTooltip
          content={t(($) => $.board.add_task_tooltip)}
          trigger={
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.board.add_task_tooltip)}
              className="mt-1 w-full rounded-md text-muted-foreground hover:text-foreground"
              onClick={handleAdd}
            >
              <Plus className="size-3.5" />
            </Button>
          }
        />
      )}
    </div>
  );
}

function SwimLaneHiddenColumnsPanel({
  hiddenStatuses,
  statusTotals,
}: {
  hiddenStatuses: TaskStatus[];
  statusTotals: Map<TaskStatus, number>;
}) {
  return (
    <HiddenColumnsPanel
      hiddenStatuses={hiddenStatuses}
      renderRow={(status) => (
        <HiddenColumnRow
          key={status}
          status={status}
          total={statusTotals.get(status) ?? 0}
        />
      )}
    />
  );
}

function SwimLaneLoadMoreRow({
  sortedStatuses,
  gridStyle,
  myTasksOpts,
  sort,
}: {
  sortedStatuses: TaskStatus[];
  gridStyle: React.CSSProperties;
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
}) {
  return (
    <div style={gridStyle}>
      {sortedStatuses.map((status) => (
        <SwimLaneLoadMoreCell
          key={status}
          status={status}
          myTasksOpts={myTasksOpts}
          sort={sort}
        />
      ))}
    </div>
  );
}

function SwimLaneLoadMoreCell({
  status,
  myTasksOpts,
  sort,
}: {
  status: TaskStatus;
  myTasksOpts?: { scope: string; filter: MyTasksFilter };
  sort?: TaskSortParam;
}) {
  const { loadMore, hasMore, isLoading } = useLoadMoreByStatus(status, myTasksOpts, sort);
  if (!hasMore) return <div />;
  return <InfiniteScrollSentinel onVisible={loadMore} loading={isLoading} />;
}

/**
 * Memoized: the surface controller re-renders on loading-flag flips (e.g. a
 * query enabling when the view changes) — without memo every such flip
 * re-rendered this entire view tree (hundreds of ms). All props are
 * referentially stable useMemo/useCallback outputs from the controller.
 */
export const SwimLaneView = memo(SwimLaneViewImpl);
