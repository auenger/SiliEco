import type { Label } from "./label";
import type { TaskPropertyValues } from "./property";

export type TaskStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type TaskPriority = "urgent" | "high" | "medium" | "low" | "none";

export type TaskAssigneeType = "member" | "agent" | "squad";

export interface TaskReaction {
  id: string;
  task_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

/**
 * Per-task metadata is a flat KV map agents use to record pipeline state
 * (PR number, pipeline_status, waiting_on, ...). Values are primitives only —
 * string / number / bool — enforced by both the API and the DB. Always
 * present in responses (empty object when unset) so reads don't need a
 * nil guard on the parent field.
 */
export type TaskMetadataValue = string | number | boolean;
export type TaskMetadata = Record<string, TaskMetadataValue>;

export interface Task {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_type: TaskAssigneeType | null;
  assignee_id: string | null;
  creator_type: TaskAssigneeType;
  creator_id: string;
  parent_task_id: string | null;
  project_id: string | null;
  /** Optional SOP execution coordinates; null keeps the plain Task lifecycle. */
  workflow_instance_id?: string | null;
  workflow_stage_id?: string | null;
  workflow_invalidated_by_rework_id?: string | null;
  position: number;
  // Ordered barrier group among sibling sub-tasks (null = unstaged). The
  // parent assignee is notified/woken only when every sub-task in a stage
  // finishes; see core/internal/handler/task_child_done.go.
  stage: number | null;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @silieco/core/tasks/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  metadata: TaskMetadata;
  // Custom property values keyed by property definition id. Always present
  // in responses (empty object when unset), mirroring `metadata`.
  properties: TaskPropertyValues;
  reactions?: TaskReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
  revision?: string;
}
