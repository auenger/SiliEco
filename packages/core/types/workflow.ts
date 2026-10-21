import type { Task } from "./issue";

export type WorkflowStatus = "draft" | "published" | "archived";
export type WorkflowVersionStatus = "draft" | "published" | "superseded";
export type WorkflowInstanceStatus =
  | "draft"
  | "active"
  | "waiting"
  | "completed"
  | "cancelled";
export type WorkflowGateType = "none" | "human" | "agent" | "hybrid";

export type WorkflowObject = Record<string, unknown>;

export interface WorkflowStage {
  execution_mode?: string;
  executor_id?: string | null;
  id: string;
  workspace_id: string;
  workflow_version_id: string;
  stable_key: string;
  name: string;
  description: string | null;
  position: number;
  rework_policy?: WorkflowObject;
  retry_policy?: WorkflowObject;
  completion_rule: WorkflowObject;
  input_spec: WorkflowObject;
  input_bindings?: WorkflowObject;
  output_spec: WorkflowObject;
  required_skills: string[];
  allowed_mcp_server_ids?: string[] | null;
  gate: WorkflowObject & { type?: WorkflowGateType };
  rollback_stage_key: string | null;
  created_at: string;
}

export interface WorkflowInstanceStage {
  execution_mode?: string;
  executor_id?: string | null;
  id: string;
  workspace_id: string;
  workflow_instance_id: string;
  source_stage_id: string | null;
  stable_key: string;
  name: string;
  description: string | null;
  position: number;
  rework_policy?: WorkflowObject;
  retry_policy?: WorkflowObject;
  completion_rule: WorkflowObject;
  input_spec: WorkflowObject;
  input_bindings?: WorkflowObject;
  output_spec: WorkflowObject;
  required_skills: string[];
  required_skill_revisions?: Record<string, string>;
  allowed_mcp_server_ids?: string[] | null;
  gate: WorkflowObject & { type?: WorkflowGateType };
  rollback_stage_key: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowVersion {
  id: string;
  workspace_id: string;
  workflow_id: string;
  version: number;
  status: WorkflowVersionStatus;
  created_by: string;
  published_at: string | null;
  created_at: string;
  stages: WorkflowStage[];
}

export interface Workflow {
  id: string;
  workspace_id: string;
  project_id: string | null;
  name: string;
  description: string | null;
  status: WorkflowStatus;
  current_version_id: string | null;
  current_version?: WorkflowVersion;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface RecordWorkflowHumanDecisionRequest {
  expected_result_revision: number;
  expected_basis_hash: string;
  outcome: "approved" | "rejected";
  note?: string;
}

export interface WorkflowHumanReview {
  execution_id: string;
  result_revision: number;
  basis_hash: string;
  issues: { id: string; title: string; status: string; updated_at: string }[];
  calls: { id: string; issue_id: string | null; agent_id: string; status: string }[];
}

export interface WorkflowGateDecision {
  invalidated_by_rework_id?: string | null;
  execution_id?: string | null;
  result_revision?: number | null;
  review_step?: string | null;
  basis_hash?: string | null;
  id: string;
  workflow_instance_id: string;
  from_stage_id: string;
  to_stage_id: string | null;
  outcome: "approved" | "rejected";
  actor_type: "member" | "agent";
  actor_id: string;
  note: string | null;
  created_at: string;
}

export interface WorkflowRework {
 id: string;
 source_stage_id: string;
 target_stage_id: string;
 source_execution_id: string | null;
 plan_revision: number;
 actor_type: "member" | "agent";
 actor_id: string;
 reason: string;
 created_at: string;
}

export interface WorkflowInstance {
 reworks?: WorkflowRework[];
  state_token?: string | null;
  inputs?: WorkflowObject;
  id: string;
  workspace_id: string;
  workflow_id: string;
  workflow_version_id: string;
  title: string;
  description: string | null;
  revision: number;
  can_edit: boolean;
  source_version: number;
  status: WorkflowInstanceStatus;
  current_stage_id: string | null;
  current_stage_name: string | null;
  current_stage_index: number | null;
  stage_count: number;
  project_id: string | null;
  created_by: string;
  started_at: string | null;
  completed_at: string | null;
  task_count: number;
  archived_at: string | null;
  archived_by: string | null;
  created_at: string;
  updated_at: string;
  stages?: WorkflowInstanceStage[];
  tasks?: Task[];
  decisions?: WorkflowGateDecision[];
  changes?: WorkflowInstanceChange[];
}

export interface WorkflowInstanceChange {
  id: string;
  revision: number;
  changed_by: string;
  change_note: string | null;
  before_plan: WorkflowObject;
  after_plan: WorkflowObject;
  created_at: string;
}

export interface WorkflowStageInput {
  execution_mode?: string;
  executor_id?: string | null;
  id?: string;
  stable_key?: string;
  name: string;
  description?: string | null;
  rework_policy?: WorkflowObject;
  retry_policy?: WorkflowObject;
  completion_rule?: WorkflowObject;
  input_spec?: WorkflowObject;
  input_bindings?: WorkflowObject;
  output_spec?: WorkflowObject;
  required_skills?: string[];
  allowed_mcp_server_ids?: string[] | null;
  gate?: WorkflowObject & { type?: WorkflowGateType };
  rollback_stage_key?: string | null;
}

export interface UpdateWorkflowInstancePlanRequest {
  expected_revision: number;
  title: string;
  description?: string | null;
  change_note?: string | null;
  stages: WorkflowStageInput[];
}

export interface CreateWorkflowRequest {
  project_id?: string | null;
  name: string;
  description?: string | null;
  publish?: boolean;
  stages: WorkflowStageInput[];
}

export interface UpdateWorkflowRequest {
  project_id?: string;
  name?: string;
  description?: string | null;
}

export interface CreateWorkflowVersionRequest {
  publish?: boolean;
  stages: WorkflowStageInput[];
}

export interface CreateWorkflowInstanceRequest {
  inputs?: WorkflowObject;
  title: string;
  description?: string | null;
  version_id?: string;
  project_id?: string | null;
  start?: boolean;
}

export interface TransitionWorkflowInstanceRequest {
  expected_state_token: string;
  target_stage_id?: string;
  outcome?: "approved" | "rejected";
  note?: string | null;
}

export interface ListWorkflowsResponse {
  workflows: Workflow[];
  total: number;
}

export interface ListWorkflowVersionsResponse {
  versions: WorkflowVersion[];
  total: number;
}

export interface ListWorkflowInstancesResponse {
  instances: WorkflowInstance[];
  total: number;
}

export interface ListWorkflowTasksResponse {
  tasks: Task[];
  total: number;
}

export interface WorkflowStageRun {
  id: string;
  task_id: string;
  task_title: string;
  agent_id: string;
  agent_name: string | null;
  status: string;
  trigger_summary: string | null;
  trigger_source: string | null;
  trigger_user_id: string | null;
  accountable_user_id: string | null;
  started_at: string | null;
  completed_at: string | null;
  result?: unknown;
  error: string | null;
  failure_reason: string | null;
  execution_manifest?: Record<string, unknown>;
  created_at: string;
  messages_path: string;
  worktree_branch: string | null;
  worktree_base_sha: string | null;
  worktree_final_sha: string | null;
  worktree_delivery_status: "committed" | "preserved" | null;
}

export interface WorkflowStageTranscriptResponse {
  runs: WorkflowStageRun[];
  total: number;
  limit: number;
  offset: number;
}


export interface WorkflowExecution {
  failure_code?: string | null;
  failure_detail?: string | null;
  id: string;
  stage_id: string;
  attempt: number;
  plan_revision: number;
  executor_id: string;
  main_issue_id: string | null;
  main_task_id: string | null;
  status: string;
  result_revision: number;
}

export interface WorkflowResult {
  id: string;
  execution_id: string;
  revision: number;
  data: unknown;
  artifacts: {
    id: string;
    artifact_id: string;
    revision: number;
    name: string;
    media_type: string;
    size_bytes: number;
    checksum: string;
  }[];
}


export interface CorrectWorkflowExecutionInputsRequest {
  expected_inputs: Record<string, unknown>;
  inputs: Record<string, unknown>;
}

export interface WorkflowInputRecovery {
  execution_id: string;
  instance_id: string;
  attempt: number;
  status: "pending";
}

export interface ReworkWorkflowExecutionRequest {
  expected_revision: number;
  target_stage_id: string;
  reason: string;
}

export interface WorkflowExecutionRework {
  rework_id?: string;
  execution_id: string | null;
  source_execution_id: string;
  instance_id: string;
  stage_id: string;
  attempt: number | null;
  status: "awaiting_work" | "pending" | "queued" | "running" | "awaiting_review" | "succeeded" | "blocked" | "failed" | "cancelled";
}

export interface WorkflowAgentReviewStatus {
  execution_id: string;
  phase: string;
  request_id: string | null;
  reviewer_id: string | null;
  result_revision: number;
  basis_hash: string | null;
  call: {id: string; status: string; failure_reason: string | null; attempt: number; max_attempts: number} | null;
  can_recover: boolean;
  recovery_attempts: number;
  retry_at: string | null;
}

export interface RecoverWorkflowAgentReviewRequest {
  failed_task_id: string;
  expected_result_revision: number;
  expected_basis_hash: string;
  reason: string;
}

export interface WorkflowAgentReviewRecovery {
  recovery_id: string;
  instance_id: string;
  execution_id: string;
  request_id: string;
  failed_task_id: string;
  task_id: string;
  allowed_attempts: number;
  max_attempts: number;
}
