import type { Task, TaskMetadata, TaskStatus, TaskPriority, TaskAssigneeType } from "./task";
import type { MemberRole } from "./workspace";
import type { Project } from "./project";

// Task API
export interface CreateTaskRequest {
  title: string;
  description?: string;
  status?: TaskStatus;
  priority?: TaskPriority;
  assignee_type?: TaskAssigneeType;
  assignee_id?: string;
  parent_task_id?: string;
  project_id?: string;
  workflow_instance_id?: string;
  workflow_stage_id?: string;
  /** Ordered stage (>= 1) grouping this sub-task under its parent. */
  stage?: number;
  start_date?: string;
  due_date?: string;
  attachment_ids?: string[];
  /** Task-scoped label IDs to attach in the same transaction as the create.
   *  Unknown or non-task ids are rejected by the server with 400. */
  label_ids?: string[];
}

export interface UpdateTaskRequest {
  expected_revision?: string;
  title?: string;
  description?: string;
  status?: TaskStatus;
  priority?: TaskPriority;
  assignee_type?: TaskAssigneeType | null;
  assignee_id?: string | null;
  position?: number;
  start_date?: string | null;
  due_date?: string | null;
  parent_task_id?: string | null;
  project_id?: string | null;
  workflow_instance_id?: string | null;
  workflow_stage_id?: string | null;
  /** Ordered stage (>= 1); null clears it (unstaged). */
  stage?: number | null;
  /** Attachment IDs to bind to this task alongside the description update.
   *  Used by the description editor to register newly uploaded files so they
   *  surface in `taskAttachments` and keep their preview Eye on refresh. */
  attachment_ids?: string[];
  /** Skip starting the agent run this write would trigger ("暂时不启动",
   *  SILI-3375). The assignee/status change still applies. Control field —
   *  strip from optimistic cache patches; never written onto the Task. */
  suppress_run?: boolean;
  /** Free-text handoff instruction injected into the started run's opening
   *  context (SILI-3375). Only consumed when a run actually starts. Control
   *  field — strip from optimistic cache patches. */
  handoff_note?: string;
}

/**
 * Server-owned drag intent. The client may use a provisional position for its
 * optimistic animation, but the canonical position is derived from these
 * workspace-scoped neighbors by `POST /api/tasks/:id/move`.
 */
export interface MoveTaskRequest
  extends Pick<
    UpdateTaskRequest,
    | "status"
    | "assignee_type"
    | "assignee_id"
    | "parent_task_id"
    | "project_id"
  > {
  before_id: string | null;
  after_id: string | null;
}

/** Inputs to `POST /api/tasks/preview-trigger`. A nil prospective field means
 *  "leave unchanged"; `isCreate` previews a not-yet-persisted task. */
export interface TaskTriggerPreviewParams {
  taskIds?: string[];
  isCreate?: boolean;
  assigneeType?: TaskAssigneeType | null;
  assigneeId?: string | null;
  status?: TaskStatus;
}

/** One task that WILL start a run under the prospective write. `agent_id` is
 *  the runnable agent (squad leader for squads). `handoff_supported` is the
 *  soft-gate signal: false when the target runtime is too old to render a
 *  handoff note (gray the note box; the assignment still works). */
export interface TaskTriggerPreviewItem {
  task_id: string;
  agent_id: string;
  source: string;
  handoff_supported: boolean;
}

export interface TaskTriggerPreview {
  triggers: TaskTriggerPreviewItem[];
  total_count: number;
}

export interface ListTasksParams {
  limit?: number;
  offset?: number;
  workspace_id?: string;
  /** Flat-table quick search. Matches task title words or an exact task number. */
  q?: string;
  status?: TaskStatus;
  /** Multi-value table facet. OR within the field. */
  statuses?: TaskStatus[];
  priority?: TaskPriority;
  /** Multi-value table facet. OR within the field. */
  priorities?: TaskPriority[];
  assignee_id?: string;
  assignee_ids?: string[];
  /**
   * Narrow to tasks assigned to the given actor kinds (member / agent /
   * squad). Same semantics as `ListGroupedTasksParams.assignee_types` —
   * powers the workspace Members/Agents tabs server-side.
   */
  assignee_types?: TaskAssigneeType[];
  creator_id?: string;
  project_id?: string;
  /** Actor-aware table facets. OR within each field. */
  assignee_filters?: TaskActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: TaskActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  /** Restrict the window to root tasks instead of filtering loaded pages. */
  top_level_only?: boolean;
  /**
   * Hard restriction of the window to the given task ids (the table's
   * agents-working facet sends the live running-task set). An EMPTY array is
   * meaningful and yields an EMPTY window — omit the field entirely for "no
   * restriction".
   */
  ids?: string[];
  /**
   * Widen the assignee filter to tasks where the user is the *indirect*
   * assignee — assignee is one of the user's owned agents, or a squad that
   * involves the user (human member / leader-via-owned-agent / agent member
   * owned by the user). Direct member assignment is intentionally excluded:
   * `involves_user_id` and `assignee_id=<user>` (tab "Assigned to me") produce
   * disjoint result sets by construction.
   */
  involves_user_id?: string;
  /** JSONB containment filter on `task.metadata`. AND across keys. */
  metadata?: TaskMetadata;
  /** Custom-property filter: definition id → accepted values (option ids or
   *  "true"/"false" for checkbox). OR within a definition, AND across. */
  properties?: Record<string, string[]>;
  open_only?: boolean;
  /**
   * Restrict the result to tasks with at least one of `start_date` /
   * `due_date` set. Used by the Project Gantt view so it doesn't have to
   * page through every task on the project just to discard the unscheduled
   * majority on the client.
   */
  scheduled?: boolean;
  date_field?: "created_at" | "updated_at";
  date_start?: string;
  date_end?: string;
  sort_by?:
    | "position"
    | "status"
    | "priority"
    | "title"
    | "created_at"
    | "updated_at"
    | "start_date"
    | "due_date"
    | `property:${string}`;
  sort_direction?: "asc" | "desc";
}

export interface TaskActorRef {
  type: TaskAssigneeType;
  id: string;
}

export interface ListGroupedTasksParams {
  group_by: "assignee";
  limit?: number;
  offset?: number;
  workspace_id?: string;
  statuses?: TaskStatus[];
  priorities?: TaskPriority[];
  assignee_types?: TaskAssigneeType[];
  assignee_id?: string;
  assignee_ids?: string[];
  creator_id?: string;
  project_id?: string;
  /** See `ListTasksParams.involves_user_id` — same semantics. */
  involves_user_id?: string;
  /** JSONB containment filter on `task.metadata`. AND across keys. */
  metadata?: TaskMetadata;
  /** Custom-property filter: definition id → accepted values (option ids or
   *  "true"/"false" for checkbox). OR within a definition, AND across. */
  properties?: Record<string, string[]>;
  assignee_filters?: TaskActorRef[];
  include_no_assignee?: boolean;
  creator_filters?: TaskActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  group_assignee_type?: TaskAssigneeType | "none";
  group_assignee_id?: string;
  date_field?: "created_at" | "updated_at";
  date_start?: string;
  date_end?: string;
  sort_by?:
    | "position"
    | "status"
    | "priority"
    | "title"
    | "created_at"
    | "updated_at"
    | "start_date"
    | "due_date"
    | `property:${string}`;
  sort_direction?: "asc" | "desc";
}

/** Raw backend response shape for `GET /api/tasks`. */
export interface ListTasksResponse {
  tasks: Task[];
  total: number;
}

export interface TaskAssigneeGroup {
  id: string;
  assignee_type: TaskAssigneeType | null;
  assignee_id: string | null;
  tasks: Task[];
  total: number;
}

/** Raw backend response shape for `GET /api/tasks/grouped?group_by=assignee`. */
export interface GroupedTasksResponse {
  groups: TaskAssigneeGroup[];
}

/** Canonical Task API names. Legacy Task names remain wire-compatible. */

// Server-authoritative Table query contract. Membership, grouping and counts
// are evaluated against the complete result set; the browser only owns view
// state such as collapsed groups/parents.
export type TaskTableScope =
  | { kind: "workspace"; assignee_types?: TaskAssigneeType[] }
  | { kind: "project"; project_id: string }
  | { kind: "assignee"; actor: TaskActorRef }
  | { kind: "creator"; actor: TaskActorRef }
  | { kind: "my"; relation: "assigned" | "created" | "involved" | "any" };

export interface TaskTableFilters {
  statuses?: TaskStatus[];
  priorities?: TaskPriority[];
  assignees?: TaskActorRef[];
  include_no_assignee?: boolean;
  creators?: TaskActorRef[];
  project_ids?: string[];
  include_no_project?: boolean;
  label_ids?: string[];
  properties?: Record<string, string[]>;
  date?: {
    field: "created_at" | "updated_at";
    start: string;
    end: string;
  };
  working_only?: boolean;
  /** Match the running-task task projection returned by
   *  `/api/working-agents`. An explicit empty list matches nothing. */
  working_task_ids?: string[];
  include_sub_tasks?: boolean;
}

export type TaskTableSortField =
  | "position"
  | "status"
  | "priority"
  | "title"
  | "created_at"
  | "updated_at"
  | "start_date"
  | "due_date"
  | `property:${string}`;

export interface TaskTableQuerySpec {
  scope: TaskTableScope;
  filters: TaskTableFilters;
  search?: string;
  sort: {
    field: TaskTableSortField;
    direction: "asc" | "desc";
  };
}

export type TaskTableGroupSpec =
  | { kind: "none" }
  | { kind: "status" }
  | { kind: "assignee" }
  | { kind: "project" }
  | { kind: "parent" }
  | {
      kind: "compound";
      primary: "assignee" | "project" | "parent";
      secondary: "status";
      /** Optional visible secondary buckets. When present, the server pages
       * only primary groups that contain at least one matching card and
       * returns `total` for that complete visible result set. */
      secondary_values?: TaskStatus[];
    }
  | { kind: "property"; property_id: string; include_empty?: boolean };

/** Response-side actor reference. Kept open for forward compatibility: an
 * installed desktop client may receive a new actor kind from a newer server. */
export interface TaskTableActorRef {
  type: string;
  id: string;
}

export interface TaskTableParentRef {
  id: string;
  number: number;
  identifier: string;
  title: string;
  status: string;
}

export type TaskTableGroupValue =
  | { kind: "status"; status: string }
  | { kind: "assignee"; actor: TaskTableActorRef | null }
  | { kind: "project"; project_id: string | null }
  | {
      kind: "parent";
      parent_id: string | null;
      parent: TaskTableParentRef | null;
      value_state: "value" | "unavailable" | "unset";
    }
  | {
      kind: "property";
      property_id: string;
      value?: string | boolean | null;
      value_state: "value" | "unavailable" | "unset";
    };

export interface TaskTableGroupDescriptor {
  key: string;
  value: TaskTableGroupValue;
  count: number;
  /** Present for compound groups. These opaque keys can be passed straight
   * back to `/table/rows`; clients must not reconstruct them. */
  secondary_groups?: TaskTableGroupDescriptor[];
}

export interface TaskTablePageRequest {
  limit?: number;
  cursor?: string | null;
}

export interface TaskTableGroupsRequest {
  query: TaskTableQuerySpec;
  group: Exclude<TaskTableGroupSpec, { kind: "none" }>;
  page?: TaskTablePageRequest;
}

export interface TaskTableGroupsResponse {
  query_fingerprint: string;
  total: number;
  groups: TaskTableGroupDescriptor[];
  next_cursor: string | null;
}

export interface TaskTableRowsRequest {
  query: TaskTableQuerySpec;
  group: TaskTableGroupSpec;
  group_key: string | null;
  hierarchy: { enabled: boolean };
  parent_id: string | null;
  page?: TaskTablePageRequest;
}

export interface TaskTableRow {
  task: Task;
  direct_child_count: number;
}

export interface TaskTableRowsResponse {
  query_fingerprint: string;
  group_key: string | null;
  parent_id: string | null;
  total: number;
  rows: TaskTableRow[];
  branch_total: number;
  next_cursor: string | null;
}

export type TaskTableFacetSpec =
  | { kind: "status" }
  | { kind: "priority" }
  | { kind: "assignee" }
  | { kind: "creator" }
  | { kind: "project" }
  | { kind: "label" }
  | { kind: "property"; property_id: string };

export interface TaskTableFacetsRequest {
  query: TaskTableQuerySpec;
  facets: TaskTableFacetSpec[];
  /** Existing callers default to true. Count-only UIs can skip the extra scan. */
  include_total?: boolean;
}

export interface TaskTableFacetValue {
  key: string;
  count: number;
}

export interface TaskTableFacet {
  kind: TaskTableFacetSpec["kind"];
  property_id?: string;
  values: TaskTableFacetValue[];
}

export interface TaskTableFacetsResponse {
  query_fingerprint: string;
  total: number;
  facets: TaskTableFacet[];
}

/** Per-status bucket in the paginated task cache. `total` is the server count (all pages), not the length of `tasks`. */
export interface TaskStatusBucket {
  tasks: Task[];
  total: number;
}

/**
 * Frontend cache shape for the task list. Data is bucketed by status so
 * each column can paginate independently. Assembled from per-status
 * `api.listTasks` responses by the query functions in `tasks/queries.ts`.
 */
export interface ListTasksCache {
  byStatus: Partial<Record<TaskStatus, TaskStatusBucket>>;
}

export interface SearchTaskResult extends Task {
  match_source: "title" | "description" | "comment";
  matched_snippet?: string;
  matched_description_snippet?: string;
  matched_comment_snippet?: string;
}

export interface SearchTasksResponse {
  tasks: SearchTaskResult[];
  total: number;
}

export interface SearchProjectResult extends Project {
  match_source: "title" | "description";
  matched_snippet?: string;
}

export interface SearchProjectsResponse {
  projects: SearchProjectResult[];
  total: number;
}

export interface UpdateMeRequest {
  name?: string;
  avatar_url?: string;
  language?: string;
  /** Free-form self-description (max 2000 chars). Pass "" to clear. */
  profile_description?: string;
  /** IANA tz to pin; "" clears back to browser-tz; undefined leaves untouched. */
  timezone?: string;
}

export interface CreateMemberRequest {
  email: string;
  role?: MemberRole;
}

export interface UpdateMemberRequest {
  role: MemberRole;
}

// Personal Access Tokens
export interface PersonalAccessToken {
  id: string;
  name: string;
  token_prefix: string;
  expires_at: string | null;
  last_used_at: string | null;
  created_at: string;
}

export interface CreatePersonalAccessTokenRequest {
  name: string;
  expires_in_days?: number;
}

export interface CreatePersonalAccessTokenResponse extends PersonalAccessToken {
  token: string;
}

// Pagination
export interface PaginationParams {
  limit?: number;
  offset?: number;
}
