import type { Task, TaskMetadata, TaskReaction } from "./task";
import type { TaskProperty, TaskPropertyValues } from "./property";
import type { Agent } from "./agent";
import type { InboxItem } from "./inbox";
import type { Comment, Reaction } from "./comment";
import type { TimelineEntry } from "./activity";
import type { Workspace, MemberWithUser, Invitation } from "./workspace";
import type { Project } from "./project";
import type { Label } from "./label";

// WebSocket event types (matching Go server protocol/events.go)
export type WSEventType =
  | "task:created"
  | "task:updated"
  | "task:deleted"
  | "comment:created"
  | "comment:updated"
  | "comment:deleted"
  | "comment:resolved"
  | "comment:unresolved"
  | "agent:status"
  | "agent:created"
  | "agent:archived"
  | "agent:restored"
  | "run:queued"
  | "run:dispatch"
  | "run:running"
  | "run:waiting_local_directory"
  | "run:progress"
  | "run:completed"
  | "run:failed"
  | "run:message"
  | "run:cancelled"
  | "inbox:new"
  | "inbox:read"
  | "inbox:archived"
  | "inbox:unarchived"
  | "inbox:batch-read"
  | "inbox:batch-archived"
  | "workspace:updated"
  | "workspace:deleted"
  | "member:added"
  | "member:updated"
  | "member:removed"
  | "daemon:heartbeat"
  | "daemon:register"
  | "skill:created"
  | "skill:updated"
  | "skill:deleted"
  | "subscriber:added"
  | "subscriber:removed"
  | "activity:created"
  | "reaction:added"
  | "reaction:removed"
  | "task_reaction:added"
  | "task_reaction:removed"
  | "chat:message"
  | "chat:done"
  | "chat:cancel_finalized"
  | "chat:session_read"
  | "chat:session_deleted"
  | "chat:session_updated"
  | "project:created"
  | "project:updated"
  | "project:deleted"
  | "workflow:created"
  | "workflow:updated"
  | "workflow_instance:created"
  | "workflow_instance:updated"
  | "workflow_task:changed"
  | "squad:created"
  | "squad:updated"
  | "squad:deleted"
  | "label:created"
  | "label:updated"
  | "label:deleted"
  | "task_labels:changed"
  | "task_metadata:changed"
  | "task_properties:changed"
  | "property:created"
  | "property:updated"
  | "pin:created"
  | "pin:deleted"
  | "pin:reordered"
  | "invitation:created"
  | "invitation:accepted"
  | "invitation:declined"
  | "invitation:revoked"
  | "github_installation:created"
  | "github_installation:deleted"
  | "pull_request:linked"
  | "pull_request:updated"
  | "pull_request:unlinked";

export interface WSMessage<T = unknown> {
  type: WSEventType;
  payload: T;
  actor_id?: string;
  actor_type?: string;
}

export interface TaskCreatedPayload {
  task: Task;
}

export interface TaskUpdatedPayload {
  task: Task;
  // The server stamps task:updated with which fields actually changed
  // (core/internal/handler/task.go publish). assignee_changed lets the
  // realtime layer keep filtered myList caches in place on a non-membership
  // change instead of refetching; status_changed lets it reconcile board column
  // counts when a status change lands on an off-screen (unloaded) task;
  // project_changed lets it drop a moved task from the old project's filtered
  // list (the client-side cache diff is unreliable after an optimistic local
  // move — SILI-3669 / #4548). Other change flags are present on the wire too and
  // can be surfaced here when needed.
  assignee_changed?: boolean;
  status_changed?: boolean;
  project_changed?: boolean;
}

export interface TaskDeletedPayload {
  task_id: string;
}

export interface TaskLabelsChangedPayload {
  task_id: string;
  labels: Label[];
}

export interface TaskMetadataChangedPayload {
  task_id: string;
  metadata: TaskMetadata;
}

export interface TaskPropertiesChangedPayload {
  task_id: string;
  properties: TaskPropertyValues;
}

export interface PropertyChangedPayload {
  property: TaskProperty;
}

export interface AgentStatusPayload {
  agent: Agent;
}

export interface AgentCreatedPayload {
  agent: Agent;
}

export interface AgentArchivedPayload {
  agent: Agent;
}

export interface AgentRestoredPayload {
  agent: Agent;
}

export interface InboxNewPayload {
  item: InboxItem;
}

export interface InboxReadPayload {
  item_id: string;
  recipient_id: string;
}

export interface InboxArchivedPayload {
  item_id: string;
  recipient_id: string;
}

export interface InboxUnarchivedPayload {
  item_id: string;
  task_id: string | null;
  recipient_id: string;
}

export interface InboxBatchReadPayload {
  recipient_id: string;
  count: number;
}

export interface InboxBatchArchivedPayload {
  recipient_id: string;
  count: number;
}

export interface CommentCreatedPayload {
  comment: Comment;
}

export interface CommentUpdatedPayload {
  comment: Comment;
}

export interface CommentDeletedPayload {
  comment_id: string;
  task_id: string;
}

export interface CommentResolvedPayload {
  comment: Comment;
}

export interface CommentUnresolvedPayload {
  comment: Comment;
}

export interface WorkspaceUpdatedPayload {
  workspace: Workspace;
}

export interface WorkspaceDeletedPayload {
  workspace_id: string;
}

export interface MemberUpdatedPayload {
  member: MemberWithUser;
}

export interface MemberAddedPayload {
  member: MemberWithUser;
  workspace_id: string;
  workspace_name?: string;
}

export interface MemberRemovedPayload {
  member_id: string;
  user_id: string;
  workspace_id: string;
}

export interface SubscriberAddedPayload {
  task_id: string;
  user_type: string;
  user_id: string;
  reason: string;
}

export interface SubscriberRemovedPayload {
  task_id: string;
  user_type: string;
  user_id: string;
}

export interface ActivityCreatedPayload {
  task_id: string;
  entry: TimelineEntry;
}

export interface AgentRunMessagePayload {
  run_id: string;
  task_id: string;
  chat_session_id?: string;
  seq: number;
  type: "text" | "thinking" | "tool_use" | "tool_result" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  created_at?: string;
}

export interface RunQueuedPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
}

export interface RunProgressPayload {
  run_id: string;
  task_id?: string;
  chat_session_id?: string;
  summary: string;
  step?: number;
  total?: number;
}

export interface RunDispatchPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  runtime_id: string;
  chat_session_id?: string;
}

export interface RunRunningPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
}

// run:waiting_local_directory fires when the daemon dequeues a task but
// can't immediately acquire the on-disk path lock — another task on this
// daemon is already executing in the same local_directory. The optional
// `wait_reason` mirrors the server-side hint (path / holder task id), but
// is not yet surfaced end-to-end; the UI today only reads the status.
export interface RunWaitingLocalDirectoryPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
  wait_reason?: string;
}

export interface RunCompletedPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
}

export interface RunFailedPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
}

export interface RunCancelledPayload {
  run_id: string;
  agent_id: string;
  task_id: string;
  chat_session_id?: string;
  status: string;
}

export interface ReactionAddedPayload {
  reaction: Reaction;
  task_id: string;
}

export interface ReactionRemovedPayload {
  comment_id: string;
  task_id: string;
  emoji: string;
  actor_type: string;
  actor_id: string;
}

export interface TaskReactionAddedPayload {
  reaction: TaskReaction;
  task_id: string;
}

export interface TaskReactionRemovedPayload {
  task_id: string;
  emoji: string;
  actor_type: string;
  actor_id: string;
}

export interface ChatMessageEventPayload {
  chat_session_id: string;
  message_id: string;
  role: "user" | "assistant";
  content: string;
  run_id?: string;
  created_at: string;
}

export interface ChatDonePayload {
  chat_session_id: string;
  run_id: string;
  /**
   * Server populates these from the freshly-persisted assistant ChatMessage
   * row so the WS handler can write it into the messages cache inline. Older
   * servers (pre-#2123) only sent chat_session_id + run_id; treat every field
   * below as optional and fall back to a refetch when absent.
   */
  message_id?: string;
  content?: string;
  elapsed_ms?: number;
  created_at?: string;
  /** Agent author for group-chat replies. */
  sender_agent_id?: string;
  /**
   * "message" (default) or "no_response" — a completed direct-chat turn with
   * no text reply (SILI-4351). Optional/additive: older servers omit it, so the
   * consumer defaults to "message". Because direct-chat completion now always
   * persists exactly one assistant row, message_id/content/created_at are
   * populated alongside this even for a no_response turn.
   */
  message_kind?: import("./chat").ChatMessageKind;
}

/**
 * Deferred outcome of a cancelled chat task (#5219). The cancel HTTP response
 * cannot carry it — the empty/non-empty judgment settles only after the
 * daemon's transcript flush — so the server broadcasts it here instead:
 * outcome "stopped" describes a freshly-persisted "Stopped." assistant row
 * (ChatDonePayload-shaped fields), outcome "restored" is a content-free
 * invalidation hint — the deleted prompt itself is durable server-side and
 * the initiator's client fetches it from the creator-authorized
 * draft-restores endpoint. All fields beyond the discriminator are optional —
 * treat defensively and fall back to a refetch.
 */
export interface ChatCancelFinalizedPayload {
  outcome: "stopped" | "restored";
  chat_session_id: string;
  run_id: string;
  /**
   * The user who triggered the cancelled task. Only this user's client needs
   * to refetch draft restores; treat a missing value as "not me" (fail
   * closed — the durable restore is still picked up on the next session
   * open).
   */
  initiator_user_id?: string;
  message_id?: string;
  /** "Stopped." assistant row fields — set only for outcome "stopped". */
  content?: string;
  message_kind?: import("./chat").ChatMessageKind;
  created_at?: string;
  elapsed_ms?: number;
}

export interface ChatSessionReadPayload {
  chat_session_id: string;
}

export interface ChatSessionDeletedPayload {
  chat_session_id: string;
}

export interface ProjectCreatedPayload {
  project: Project;
}

export interface ProjectUpdatedPayload {
  project: Project;
}

export interface ProjectDeletedPayload {
  project_id: string;
}

export interface InvitationCreatedPayload {
  invitation: Invitation;
  workspace_name?: string;
}

export interface InvitationAcceptedPayload {
  invitation_id: string;
  member: MemberWithUser;
}

export interface InvitationDeclinedPayload {
  invitation_id: string;
  invitee_email: string;
}

export interface InvitationRevokedPayload {
  invitation_id: string;
  invitee_email: string;
}

/**
 * Maps every WSEventType to its payload interface. Events whose payload
 * shape isn't formally typed (server emits an object the client doesn't
 * meaningfully consume yet) fall back to `unknown` — callers must narrow
 * before access.
 *
 * Use via `WSEventPayload<E>` rather than indexing the map directly:
 *   const handler = (payload: WSEventPayload<"task:created">) => { ... };
 *
 * Adding a new event: extend WSEventType first (above), then append a key
 * here. TS will compile-error every WSClient.on("new:event", …) site that
 * forgets the payload shape — that's the whole point.
 */
export interface WSEventPayloadMap {
  "task:created": TaskCreatedPayload;
  "task:updated": TaskUpdatedPayload;
  "task:deleted": TaskDeletedPayload;
  "task_labels:changed": TaskLabelsChangedPayload;
  "task_properties:changed": TaskPropertiesChangedPayload;
  "property:created": PropertyChangedPayload;
  "property:updated": PropertyChangedPayload;
  "task_reaction:added": TaskReactionAddedPayload;
  "task_reaction:removed": TaskReactionRemovedPayload;
  "comment:created": CommentCreatedPayload;
  "comment:updated": CommentUpdatedPayload;
  "comment:deleted": CommentDeletedPayload;
  "comment:resolved": CommentResolvedPayload;
  "comment:unresolved": CommentUnresolvedPayload;
  "reaction:added": ReactionAddedPayload;
  "reaction:removed": ReactionRemovedPayload;
  "agent:status": AgentStatusPayload;
  "agent:created": AgentCreatedPayload;
  "agent:archived": AgentArchivedPayload;
  "agent:restored": AgentRestoredPayload;
  "run:queued": RunQueuedPayload;
  "run:dispatch": RunDispatchPayload;
  "run:running": RunRunningPayload;
  "run:waiting_local_directory": RunWaitingLocalDirectoryPayload;
  "run:completed": RunCompletedPayload;
  "run:failed": RunFailedPayload;
  "run:message": AgentRunMessagePayload;
  "run:cancelled": RunCancelledPayload;
  "run:progress": RunProgressPayload;
  "inbox:new": InboxNewPayload;
  "inbox:read": InboxReadPayload;
  "inbox:archived": InboxArchivedPayload;
  "inbox:unarchived": InboxUnarchivedPayload;
  "inbox:batch-read": InboxBatchReadPayload;
  "inbox:batch-archived": InboxBatchArchivedPayload;
  "workspace:updated": WorkspaceUpdatedPayload;
  "workspace:deleted": WorkspaceDeletedPayload;
  "member:added": MemberAddedPayload;
  "member:updated": MemberUpdatedPayload;
  "member:removed": MemberRemovedPayload;
  "subscriber:added": SubscriberAddedPayload;
  "subscriber:removed": SubscriberRemovedPayload;
  "activity:created": ActivityCreatedPayload;
  "chat:message": ChatMessageEventPayload;
  "chat:done": ChatDonePayload;
  "chat:cancel_finalized": ChatCancelFinalizedPayload;
  "chat:session_read": ChatSessionReadPayload;
  "chat:session_deleted": ChatSessionDeletedPayload;
  "chat:session_updated": unknown;
  "project:created": ProjectCreatedPayload;
  "project:updated": ProjectUpdatedPayload;
  "project:deleted": ProjectDeletedPayload;
  "workflow:created": unknown;
  "workflow:updated": unknown;
  "workflow_instance:created": unknown;
  "workflow_instance:updated": unknown;
  "workflow_task:changed": unknown;
  "invitation:created": InvitationCreatedPayload;
  "invitation:accepted": InvitationAcceptedPayload;
  "invitation:declined": InvitationDeclinedPayload;
  "invitation:revoked": InvitationRevokedPayload;
  // No formal payload interfaces yet — server emits domain objects clients
  // currently consume as opaque triggers (refetch on receipt).
  "daemon:heartbeat": unknown;
  "daemon:register": unknown;
  "skill:created": unknown;
  "skill:updated": unknown;
  "skill:deleted": unknown;
  "squad:created": unknown;
  "squad:updated": unknown;
  "squad:deleted": unknown;
  "label:created": unknown;
  "label:updated": unknown;
  "label:deleted": unknown;
  "pin:created": unknown;
  "pin:deleted": unknown;
  "pin:reordered": unknown;
  "github_installation:created": unknown;
  "github_installation:deleted": unknown;
  "pull_request:linked": unknown;
  "pull_request:updated": unknown;
  "pull_request:unlinked": unknown;
}

/**
 * Payload type for a given event. Lookup against WSEventPayloadMap with
 * `unknown` as the safety net — if a future WSEventType is added without
 * a map entry, callers see `unknown` (forced narrow) rather than `any`
 * (silent unsafe access).
 */
export type WSEventPayload<E extends WSEventType> =
  E extends keyof WSEventPayloadMap ? WSEventPayloadMap[E] : unknown;
