import type {
  Task,
  TaskStatus,
  TaskPriority,
  TaskAssigneeType,
} from "../types";

/**
 * Shared assignee across a selection. `{ type: null, id: null }` means every
 * selected task is unassigned — a real shared value, distinct from a mixed
 * selection (which {@link commonTaskFields} reports as `assignee: null`).
 */
export interface CommonAssignee {
  type: TaskAssigneeType | null;
  id: string | null;
}

/**
 * The status / priority / assignee shared by every task in a batch selection.
 * A field is `null` when the selection is empty or the tasks disagree
 * ("mixed"). Batch property pickers use this to reflect the real common value
 * and fall back to an empty (no-checkmark) state when the values differ,
 * instead of asserting a hardcoded default.
 */
export interface CommonTaskFields {
  status: TaskStatus | null;
  priority: TaskPriority | null;
  assignee: CommonAssignee | null;
}

/**
 * Returns the value shared by every item, or `null` when the list is empty or
 * the items disagree. Comparison is by primitive equality, so callers pass a
 * scalar key (collapse composite values to a string before calling).
 */
function sharedValue<T>(values: readonly T[]): T | null {
  if (values.length === 0) return null;
  const first = values[0]!;
  return values.every((v) => v === first) ? first : null;
}

const ASSIGNEE_KEY_SEP = "\u0000";

/**
 * Collapse a polymorphic assignee (type + id, either nullable) into a single
 * comparable key so all-unassigned tasks compare equal to each other and
 * distinct from any assigned actor.
 */
function assigneeKey(type: TaskAssigneeType | null, id: string | null): string {
  return `${type ?? ""}${ASSIGNEE_KEY_SEP}${id ?? ""}`;
}

/**
 * Derive the common status / priority / assignee of the selected tasks.
 * Pass the already-filtered selection (the tasks that are actually selected),
 * mirroring how the skill list filters its rows by `selectedIds` before
 * handing them to its batch toolbar.
 */
export function commonTaskFields(tasks: readonly Task[]): CommonTaskFields {
  const status = sharedValue(tasks.map((i) => i.status));
  const priority = sharedValue(tasks.map((i) => i.priority));

  const sharedAssigneeKey = sharedValue(
    tasks.map((i) => assigneeKey(i.assignee_type, i.assignee_id)),
  );
  const assignee =
    sharedAssigneeKey !== null && tasks.length > 0
      ? { type: tasks[0]!.assignee_type, id: tasks[0]!.assignee_id }
      : null;

  return { status, priority, assignee };
}
