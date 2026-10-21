import type { Label } from "./label";
import type { IssuePropertyValues } from "./property";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

/**
 * Per-issue metadata is a flat KV map agents use to record pipeline state
 * (PR number, pipeline_status, waiting_on, ...). Values are primitives only —
 * string / number / bool — enforced by both the API and the DB. Always
 * present in responses (empty object when unset) so reads don't need a
 * nil guard on the parent field.
 */
export type IssueMetadataValue = string | number | boolean;
export type IssueMetadata = Record<string, IssueMetadataValue>;

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  /** Optional SOP execution coordinates; null keeps the plain Task lifecycle. */
  workflow_instance_id?: string | null;
  workflow_stage_id?: string | null;
  workflow_invalidated_by_rework_id?: string | null;
  position: number;
  // Ordered barrier group among sibling sub-issues (null = unstaged). The
  // parent assignee is notified/woken only when every sub-issue in a stage
  // finishes; see core/internal/handler/issue_child_done.go.
  stage: number | null;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @silieco/core/issues/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  metadata: IssueMetadata;
  // Custom property values keyed by property definition id. Always present
  // in responses (empty object when unset), mirroring `metadata`.
  properties: IssuePropertyValues;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
  revision?: string;
}

/**
 * Canonical Silieco work-item vocabulary.
 *
 * The server still serializes the established `issue_*` field names during
 * the compatibility window, so Task is intentionally a structural alias
 * rather than a second model. New product code should import these names.
 */
export type TaskStatus = IssueStatus;
export type TaskPriority = IssuePriority;
export type TaskAssigneeType = IssueAssigneeType;
export type TaskReaction = IssueReaction;
export type TaskMetadataValue = IssueMetadataValue;
export type TaskMetadata = IssueMetadata;
export type Task = Issue;
