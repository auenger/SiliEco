import {
  pointerWithin,
  closestCenter,
  type CollisionDetection,
} from "@dnd-kit/core";
import type { Task, TaskAssigneeType, TaskStatus, UpdateTaskRequest } from "@silieco/core/types";
import type { TaskGrouping } from "@silieco/core/tasks/stores/view-store";
import { propertyIdFromViewKey } from "@silieco/core/tasks/stores/view-store";
import type { BoardColumnGroup } from "../components/board-column";

export type DragMoveTargetUpdates = Pick<
  UpdateTaskRequest,
  "status" | "assignee_type" | "assignee_id" | "position"
>;

export type DragMoveUpdates = DragMoveTargetUpdates & {
  before_id: string | null;
  after_id: string | null;
};

const UNASSIGNED_GROUP_ID = "assignee:unassigned";

export function makeKanbanCollision(groupIds: Set<string>): CollisionDetection {
  return (args) => {
    const pointer = pointerWithin(args);
    if (pointer.length > 0) {
      const items = pointer.filter((c) => !groupIds.has(c.id as string));
      if (items.length > 0) return items;
      return pointer;
    }
    return closestCenter(args);
  };
}

export function statusGroupId(status: TaskStatus): string {
  return `status:${status}`;
}

export function propertyGroupId(propertyId: string, optionId: string | null): string {
  return `property:${propertyId}:${optionId ?? "none"}`;
}

export function assigneeGroupId(
  type: TaskAssigneeType | null,
  id: string | null,
): string {
  return type && id ? `assignee:${type}:${id}` : UNASSIGNED_GROUP_ID;
}

export function getTaskGroupId(
  task: Task,
  grouping: TaskGrouping,
  knownOptionIds?: ReadonlySet<string>,
): string {
  if (grouping === "status") return statusGroupId(task.status);
  const propertyId = propertyIdFromViewKey(grouping);
  if (propertyId) {
    const value = task.properties?.[propertyId];
    let optionId = typeof value === "string" ? value : null;
    // A value referencing an option no longer in the definition (removed
    // before the in-use guard existed, or by a newer server) must bucket
    // into the No-value column — an unmatched column id would silently drop
    // the task from the board.
    if (optionId !== null && knownOptionIds && !knownOptionIds.has(optionId)) {
      optionId = null;
    }
    return propertyGroupId(propertyId, optionId);
  }
  return assigneeGroupId(task.assignee_type, task.assignee_id);
}

export function buildColumns(
  tasks: Task[],
  groups: BoardColumnGroup[],
  grouping: TaskGrouping,
  knownOptionIds?: ReadonlySet<string>,
): Record<string, string[]> {
  const cols: Record<string, string[]> = {};
  for (const group of groups) cols[group.id] = [];
  for (const task of tasks) {
    const gid = getTaskGroupId(task, grouping, knownOptionIds);
    if (cols[gid]) cols[gid].push(task.id);
  }
  return cols;
}

export function computePosition(ids: string[], activeId: string, taskMap: Map<string, Task>): number {
  const idx = ids.indexOf(activeId);
  if (idx === -1) return 0;
  const getPos = (id: string) => taskMap.get(id)?.position ?? 0;
  if (ids.length === 1) return taskMap.get(activeId)?.position ?? 0;
  if (idx === 0) return getPos(ids[1]!) - 1;
  if (idx === ids.length - 1) return getPos(ids[idx - 1]!) + 1;
  return (getPos(ids[idx - 1]!) + getPos(ids[idx + 1]!)) / 2;
}

export function getMoveAnchors(
  ids: readonly string[],
  activeId: string,
): Pick<DragMoveUpdates, "before_id" | "after_id"> {
  const index = ids.indexOf(activeId);
  return {
    before_id: index > 0 ? ids[index - 1]! : null,
    after_id: index >= 0 && index < ids.length - 1 ? ids[index + 1]! : null,
  };
}

/**
 * Insert `id` into `ids` at the slot implied by `position ASC`, reading each
 * id's position from `taskMap`. Mirrors `insertByPosition` in
 * `@silieco/core/tasks/cache-helpers` so the board's optimistic placement on
 * drop matches the cache the settle reconcile rebuilds from — otherwise the
 * card would land in one slot, then jump when local columns re-derive from TQ.
 */
export function insertIdByPosition(
  ids: string[],
  id: string,
  position: number,
  taskMap: Map<string, Task>,
): string[] {
  const idx = ids.findIndex((existing) => {
    const p = taskMap.get(existing)?.position;
    return p !== undefined && p > position;
  });
  if (idx === -1) return [...ids, id];
  return [...ids.slice(0, idx), id, ...ids.slice(idx)];
}

export function findColumn(
  columns: Record<string, string[]>,
  id: string,
  columnIds: Set<string>,
): string | null {
  if (columnIds.has(id)) return id;
  for (const [columnId, ids] of Object.entries(columns)) {
    if (ids.includes(id)) return columnId;
  }
  return null;
}

export function taskMatchesGroup(task: Task, group: BoardColumnGroup): boolean {
  if (group.status) return task.status === group.status;
  if (group.propertyId !== undefined) {
    const value = task.properties?.[group.propertyId];
    const optionId = typeof value === "string" ? value : null;
    return optionId === (group.propertyOptionId ?? null);
  }
  return (
    (task.assignee_type ?? null) === (group.assigneeType ?? null) &&
    (task.assignee_id ?? null) === (group.assigneeId ?? null)
  );
}

export function getMoveUpdates(
  group: BoardColumnGroup,
  position: number,
): DragMoveTargetUpdates {
  if (group.status) return { status: group.status, position };
  // Property columns: the value change is not part of UpdateTaskRequest —
  // the board applies it through useSetTaskProperty after the position move.
  if (group.propertyId !== undefined) return { position };
  return {
    assignee_type: group.assigneeType ?? null,
    assignee_id: group.assigneeId ?? null,
    position,
  };
}
