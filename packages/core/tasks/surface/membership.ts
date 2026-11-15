import type { Task, UpdateTaskRequest } from "../../types";
import type { MyTasksFilter } from "../queries";

/**
 * Whether an task belongs to a filtered list cache.
 *
 * `"unknown"` means the client cannot decide from the entity alone — the
 * filter's predicate lives server-side (agent-ownership graph behind
 * `involves_user_id`, the my:all relation union) or the entity is missing the
 * field the filter keys on. Callers patch on `true`, remove on `false`, and
 * patch + invalidate on `"unknown"` — the "certain → patch, uncertain →
 * invalidate" rule applied to list membership.
 */
export type TaskMembership = true | false | "unknown";

/**
 * The field groups a write can touch that move an task in or out of a
 * filtered list (assignee / project) or shift per-status bucket totals
 * (status). Creator is not here: it is immutable after create.
 */
export interface TaskChangedDims {
  assignee: boolean;
  project: boolean;
  status: boolean;
}

/**
 * Derive the changed dimensions from a mutation patch. `base` (the freshest
 * cached pre-write entity, usually the detail cache) sharpens the answer:
 * writing the same value an task already has changes nothing. Without a
 * base, any written membership field counts as changed — conservative, at
 * worst one extra list refetch on settle.
 */
export function taskChangedDims(
  patch: Partial<Task> | UpdateTaskRequest,
  base?: Task,
): TaskChangedDims {
  const has = (field: string) =>
    Object.prototype.hasOwnProperty.call(patch, field);
  const p = patch as Partial<Task>;
  return {
    assignee:
      (has("assignee_id") && (!base || base.assignee_id !== p.assignee_id)) ||
      (has("assignee_type") && (!base || base.assignee_type !== p.assignee_type)),
    project: has("project_id") && (!base || base.project_id !== p.project_id),
    status: has("status") && p.status !== undefined && (!base || base.status !== p.status),
  };
}

/**
 * Does this list's server contract depend on any of the changed dimensions?
 * `scope` is the myList scope segment from the query key (`undefined` for the
 * unfiltered workspace list). When this returns false the write cannot move
 * the task in or out of the list, so a plain field patch is a complete
 * reconcile.
 */
export function listFilterDependsOn(
  scope: string | undefined,
  filter: MyTasksFilter,
  changed: TaskChangedDims,
): boolean {
  // my:all is the union of assigned / created / involved — the assigned and
  // involved legs key on the assignee.
  if (scope === "all") return changed.assignee;
  if (
    changed.assignee &&
    (filter.assignee_id !== undefined ||
      filter.assignee_ids !== undefined ||
      filter.assignee_types !== undefined ||
      filter.involves_user_id !== undefined)
  ) {
    return true;
  }
  if (changed.project && filter.project_id !== undefined) return true;
  // creator_id filters never react to updates — creator is immutable.
  return false;
}

/**
 * Judge an task against a list's server contract. AND semantics across
 * filter fields: any definitive miss is `false`; a predicate the client
 * cannot evaluate (or a field the partial entity is missing) degrades the
 * answer to `"unknown"` instead of guessing.
 */
export function taskMatchesListFilter(
  task: Partial<Task>,
  scope: string | undefined,
  filter: MyTasksFilter,
): TaskMembership {
  // my:all — union across relations; the involved leg needs the server's
  // agent-ownership graph, so membership is never decidable client-side.
  if (scope === "all") return "unknown";

  let unknown = false;

  if (filter.assignee_id !== undefined) {
    if (task.assignee_id === undefined) unknown = true;
    else if (task.assignee_id !== filter.assignee_id) return false;
  }
  if (filter.assignee_ids !== undefined) {
    if (task.assignee_id === undefined) unknown = true;
    else if (
      task.assignee_id === null ||
      !filter.assignee_ids.includes(task.assignee_id)
    ) {
      return false;
    }
  }
  if (filter.assignee_types !== undefined) {
    if (task.assignee_type === undefined) unknown = true;
    else if (
      task.assignee_type === null ||
      !filter.assignee_types.includes(task.assignee_type)
    ) {
      return false;
    }
  }
  if (filter.creator_id !== undefined) {
    if (task.creator_id === undefined) unknown = true;
    else if (task.creator_id !== filter.creator_id) return false;
  }
  if (filter.project_id !== undefined) {
    if (task.project_id === undefined) unknown = true;
    else if (task.project_id !== filter.project_id) return false;
  }
  if (filter.involves_user_id !== undefined) {
    // Indirect-assignee predicate (owned agents / squads) — server-only.
    unknown = true;
  }

  return unknown ? "unknown" : true;
}
