-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.task_prefix, w.task_counter, w.avatar_url, w.attribution_fail_closed
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: ListDaemonWorkspaces :many
-- Daemons only need the membership set and display name to discover which
-- workspaces should have local runtimes. Keep this projection intentionally
-- narrow so the periodic consistency check never reads UI-only JSON/text
-- columns such as settings, repos, or context.
SELECT w.id, w.name
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.id ASC;

-- name: GetDaemonWorkspace :one
-- Workspace-scoped daemon tokens do not carry a user ID. This narrow lookup
-- lets them use the same endpoint without widening their token scope.
SELECT id, name
FROM workspace
WHERE id = $1;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = $1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = $1;

-- name: GetWorkspaceAttributionFailClosed :one
-- Lean read of the fail-closed attribution policy for the enqueue hot path
-- (SILI-4302 §3.5), avoiding a full workspace-row fetch.
SELECT attribution_fail_closed FROM workspace
WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspace (name, slug, description, context, task_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWorkspace :one
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    task_prefix = COALESCE(sqlc.narg('task_prefix'), task_prefix),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementTaskCounter :one
UPDATE workspace SET task_counter = task_counter + 1
WHERE id = $1
RETURNING task_counter;

-- name: LockWorkspaceForDelete :one
-- Taken first by DeleteWorkspace, before it enumerates the workspace's chat
-- sessions. LockChatSessionsByWorkspace only covers sessions that exist when it
-- runs; a CreateChatSession committing during the delete window would add one
-- the lock set never saw, and a finalizer could then insert a restore for it
-- after the sweep's snapshot — orphaning the prompt (#5219).
--
-- The delete window is held closed against new sessions by an EXPLICIT protocol,
-- not the chat_session.workspace_id FK: every session creator takes
-- LockWorkspaceForChatSessionCreate (FOR KEY SHARE) on this row first, and this
-- FOR UPDATE conflicts with it. Keeping the bar in the app layer means it does
-- not silently break if that FK is ever dropped (the codebase is moving FK
-- relationships into the application layer, SILI-3515). Lock order is
-- workspace -> chat_session -> agent_run; the finalizer never touches
-- workspace, so this cannot deadlock against it.
SELECT id FROM workspace WHERE workspace.id = $1 FOR UPDATE;

-- name: LockWorkspaceForChatSessionCreate :one
-- The creator half of the workspace delete/create protocol (#5219). Every
-- production path that inserts a chat_session takes this FOR KEY SHARE lock on the
-- parent workspace row, inside its transaction, before CreateChatSession. It
-- conflicts with DeleteWorkspace's FOR UPDATE (so a create is blocked while a
-- delete is in progress, and vice versa) but not with other creators (FOR KEY
-- SHARE locks share), so concurrent session creation stays unserialized. This
-- makes the mutual exclusion explicit rather than leaning on the workspace FK's
-- implicit database locking; this remains necessary without foreign keys.
SELECT id FROM workspace WHERE workspace.id = $1 FOR KEY SHARE;

-- name: DeleteWorkspace :exec
-- Delete the workspace and its dependent records atomically. Target CTEs retain
-- the original tenant scope; explicit deletes and detaches replace database
-- cascades. Callers hold the workspace and chat-session locks while cleaning up.
WITH RECURSIVE ws_installations AS (
    SELECT id FROM channel_installation WHERE channel_installation.workspace_id = $1
),
ws_agents AS (
    SELECT id FROM agent WHERE agent.workspace_id = $1
),
ws_skills AS (
    SELECT id FROM skill WHERE skill.workspace_id = $1
),
cleared_agent_label_assignments AS (
    DELETE FROM agent_to_label WHERE agent_to_label.agent_id IN (SELECT id FROM ws_agents)
),
cleared_skill_label_assignments AS (
    DELETE FROM skill_to_label WHERE skill_to_label.skill_id IN (SELECT id FROM ws_skills)
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM ws_installations)
    RETURNING chat_session_id
),
cleared_outbound_cards AS (
    -- channel_outbound_card_message is keyed by chat_session_id (no FK); its own
    -- chat_session rows cascade away with the workspace, so reach the cards through
    -- the just-removed chat-session bindings, which still carry the id.
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_draft_restores AS (
    -- chat_draft_restore is keyed by chat_session_id with no FK (SILI-3515) and has
    -- no reaper, while its chat_session rows cascade away with the workspace. Reach
    -- them directly through chat_session (unlike the cards above, this is not
    -- limited to channel-bound sessions) or every pending restore — each holding a
    -- user's prompt text — would outlive the workspace permanently (#5219).
    --
    -- This sweep only sees restores committed before the statement's snapshot, so
    -- the caller must already hold LockChatSessionsByWorkspace: that lock is what
    -- keeps FinalizeDeferredCancelledChat from inserting one behind it.
    DELETE FROM chat_draft_restore
    WHERE chat_session_id IN (SELECT id FROM chat_session WHERE chat_session.workspace_id = $1)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_audit AS (
    -- Purge, don't detach: the workspace is gone and channel_inbound_audit has no
    -- workspace_id and no reaper, so a detached (NULL) row would be permanently
    -- unattributable. (Reclaim, where the workspace survives, still detaches.)
    DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding WHERE channel_user_binding.workspace_id = $1
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE channel_binding_token.workspace_id = $1
),
cleared_installations AS (
    DELETE FROM channel_installation WHERE channel_installation.workspace_id = $1
),
cleared_task_properties AS (
    DELETE FROM task_property WHERE task_property.workspace_id = $1
),
deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE github_pending_check_suite.workspace_id = $1
),
ws_github_prs AS (
    SELECT id FROM github_pull_request WHERE github_pull_request.workspace_id = $1
),
cleared_github_pr_check_runs AS (
    -- github_pull_request_check_run intentionally has no FK. Remove its rows
    -- before the workspace delete cascades away the parent PR mirrors.
    DELETE FROM github_pull_request_check_run
    WHERE pr_id IN (SELECT id FROM ws_github_prs)
),
-- VCS tables (migration 213) carry no FK to workspace, so they are not cascaded
-- away by the DELETE below. Sweep the workspace's connections, mirrored PRs,
-- their task links, and CI statuses here. task_vcs_pull_request has no
-- workspace_id, so reach it through the workspace's PRs; vcs_commit_status has
-- none either, so reach it through the workspace's connections.
ws_vcs_prs AS (
    SELECT id FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = $1
),
ws_vcs_connections AS (
    SELECT id FROM vcs_connection WHERE vcs_connection.workspace_id = $1
),
cleared_vcs_pr_links AS (
    DELETE FROM task_vcs_pull_request
    WHERE pull_request_id IN (SELECT id FROM ws_vcs_prs)
),
cleared_vcs_commit_statuses AS (
    DELETE FROM vcs_commit_status
    WHERE connection_id IN (SELECT id FROM ws_vcs_connections)
),
cleared_vcs_prs AS (
    DELETE FROM vcs_pull_request WHERE vcs_pull_request.workspace_id = $1
),
cleared_vcs_connections AS (
    DELETE FROM vcs_connection WHERE vcs_connection.workspace_id = $1
),
cleared_client_usage_workspace AS (
    UPDATE client_usage_daily SET workspace_id = NULL WHERE workspace_id = $1
),
cleared_workflow_artifact_uploads AS (
    UPDATE workflow_artifact_upload SET status='deleting', expires_at=now() WHERE workspace_id=$1
),
cleared_workflow_artifact_revisions AS (
    DELETE FROM workflow_artifact_revision WHERE workflow_artifact_revision.workspace_id=$1
),
cleared_workflow_artifacts AS (
    DELETE FROM workflow_artifact WHERE workflow_artifact.workspace_id=$1
),
cleared_workflow_results AS (
    DELETE FROM workflow_execution_result WHERE workflow_execution_result.workspace_id=$1
),
cleared_workflow_execution_actions AS (
    DELETE FROM workflow_execution_action WHERE workflow_execution_action.workspace_id = $1
),
cleared_workflow_stage_executions AS (
    DELETE FROM workflow_stage_execution WHERE workflow_stage_execution.workspace_id = $1
),
cleared_workflow_review_requests AS (
 DELETE FROM workflow_review_request WHERE workflow_review_request.workspace_id=$1
),
cleared_workflow_review_recoveries AS (
 DELETE FROM workflow_review_recovery WHERE workflow_review_recovery.workspace_id=$1
),
cleared_skill_evaluation_suites AS (
    DELETE FROM skill_evaluation_suite WHERE skill_evaluation_suite.workspace_id=$1
),
cleared_skill_improvement_proposals AS (
    DELETE FROM skill_improvement_proposal WHERE skill_improvement_proposal.workspace_id=$1
),
cleared_skill_revisions AS (
    DELETE FROM skill_revision WHERE skill_revision.workspace_id=$1
),
cleared_workflow_reworks AS (
    DELETE FROM workflow_rework WHERE workflow_rework.workspace_id=$1
),
cleared_workflow_gate_decisions AS (
    DELETE FROM workflow_gate_decision WHERE workflow_gate_decision.workspace_id = $1
),
cleared_workflow_instance_changes AS (
    DELETE FROM workflow_instance_change WHERE workflow_instance_change.workspace_id = $1
),
cleared_workflow_instance_stages AS (
    DELETE FROM workflow_instance_stage WHERE workflow_instance_stage.workspace_id = $1
),
cleared_workflow_instances AS (
    DELETE FROM workflow_instance WHERE workflow_instance.workspace_id = $1
),
cleared_workflow_stages AS (
    DELETE FROM workflow_stage WHERE workflow_stage.workspace_id = $1
),
cleared_workflow_versions AS (
    DELETE FROM workflow_version WHERE workflow_version.workspace_id = $1
),
cleared_workflows AS (
    DELETE FROM workflow WHERE workflow.workspace_id = $1
),
cleanup_target_activity_log AS MATERIALIZED (
    SELECT child.* FROM "activity_log" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_agent AS MATERIALIZED (
    SELECT child.* FROM "agent" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_agent_run AS MATERIALIZED (
    SELECT child.* FROM "agent_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_agent_run_message AS MATERIALIZED (
    SELECT child.* FROM "agent_run_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_run_token AS MATERIALIZED (
    SELECT child.* FROM "agent_run_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_agent_run_usage AS MATERIALIZED (
    SELECT child.* FROM "agent_run_usage" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")
),
cleanup_target_agent_runtime AS MATERIALIZED (
    SELECT child.* FROM "agent_runtime" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_agent_skill AS MATERIALIZED (
    SELECT child.* FROM "agent_skill" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_skill parent WHERE child."skill_id" = parent."id")
),
cleanup_target_attachment AS MATERIALIZED (
    SELECT child.* FROM "attachment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."comment_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_message parent WHERE child."chat_message_id" = parent."id")
),
cleanup_target_autopilot AS MATERIALIZED (
    SELECT child.* FROM "autopilot" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_autopilot_run AS MATERIALIZED (
    SELECT child.* FROM "autopilot_run" child WHERE EXISTS (SELECT 1 FROM cleanup_target_autopilot parent WHERE child."autopilot_id" = parent."id")
),
cleanup_target_autopilot_trigger AS MATERIALIZED (
    SELECT child.* FROM "autopilot_trigger" child WHERE EXISTS (SELECT 1 FROM cleanup_target_autopilot parent WHERE child."autopilot_id" = parent."id")
),
cleanup_target_chat_message AS MATERIALIZED (
    SELECT child.* FROM "chat_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_chat_session AS MATERIALIZED (
    SELECT child.* FROM "chat_session" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_comment AS MATERIALIZED (
    SELECT child.* FROM "comment" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
    UNION
    SELECT child.* FROM "comment" child JOIN cleanup_target_comment parent ON child."parent_id" = parent."id"
),
cleanup_target_comment_reaction AS MATERIALIZED (
    SELECT child.* FROM "comment_reaction" child WHERE EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."comment_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_daemon_connection AS MATERIALIZED (
    SELECT child.* FROM "daemon_connection" child WHERE EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_daemon_token AS MATERIALIZED (
    SELECT child.* FROM "daemon_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_github_installation AS MATERIALIZED (
    SELECT child.* FROM "github_installation" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_github_pull_request AS MATERIALIZED (
    SELECT child.* FROM "github_pull_request" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_github_pull_request_check_suite AS MATERIALIZED (
    SELECT child.* FROM "github_pull_request_check_suite" child WHERE EXISTS (SELECT 1 FROM cleanup_target_github_pull_request parent WHERE child."pr_id" = parent."id")
),
cleanup_target_inbox_item AS MATERIALIZED (
    SELECT child.* FROM "inbox_item" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_lark_binding_token AS MATERIALIZED (
    SELECT child.* FROM "lark_binding_token" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_chat_session_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_chat_session_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_inbound_message_dedup AS MATERIALIZED (
    SELECT child.* FROM "lark_inbound_message_dedup" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id")
),
cleanup_target_lark_installation AS MATERIALIZED (
    SELECT child.* FROM "lark_installation" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent parent WHERE child."agent_id" = parent."id")
),
cleanup_target_lark_outbound_card_message AS MATERIALIZED (
    SELECT child.* FROM "lark_outbound_card_message" child WHERE EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id")
),
cleanup_target_lark_user_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_user_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id" AND child."workspace_id" = parent."workspace_id") OR EXISTS (SELECT 1 FROM cleanup_target_member parent WHERE child."workspace_id" = parent."workspace_id" AND child."silieco_user_id" = parent."user_id")
),
cleanup_target_member AS MATERIALIZED (
    SELECT child.* FROM "member" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_notification_preference AS MATERIALIZED (
    SELECT child.* FROM "notification_preference" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_pinned_item AS MATERIALIZED (
    SELECT child.* FROM "pinned_item" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_project AS MATERIALIZED (
    SELECT child.* FROM "project" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_project_resource AS MATERIALIZED (
    SELECT child.* FROM "project_resource" child WHERE EXISTS (SELECT 1 FROM cleanup_target_project parent WHERE child."project_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_skill AS MATERIALIZED (
    SELECT child.* FROM "skill" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_skill_file AS MATERIALIZED (
    SELECT child.* FROM "skill_file" child WHERE EXISTS (SELECT 1 FROM cleanup_target_skill parent WHERE child."skill_id" = parent."id")
),
cleanup_target_squad AS MATERIALIZED (
    SELECT child.* FROM "squad" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_squad_member AS MATERIALIZED (
    SELECT child.* FROM "squad_member" child WHERE EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id")
),
cleanup_target_task AS MATERIALIZED (
    SELECT child.* FROM "task" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_task_dependency AS MATERIALIZED (
    SELECT child.* FROM "task_dependency" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."depends_on_task_id" = parent."id")
),
cleanup_target_task_label AS MATERIALIZED (
    SELECT child.* FROM "task_label" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_task_pull_request AS MATERIALIZED (
    SELECT child.* FROM "task_pull_request" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_github_pull_request parent WHERE child."pull_request_id" = parent."id")
),
cleanup_target_task_reaction AS MATERIALIZED (
    SELECT child.* FROM "task_reaction" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_target_task_subscriber AS MATERIALIZED (
    SELECT child.* FROM "task_subscriber" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id")
),
cleanup_target_task_to_label AS MATERIALIZED (
    SELECT child.* FROM "task_to_label" child WHERE EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task_label parent WHERE child."label_id" = parent."id")
),
cleanup_target_webhook_delivery AS MATERIALIZED (
    SELECT child.* FROM "webhook_delivery" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_autopilot parent WHERE child."autopilot_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id")
),
cleanup_target_workspace AS MATERIALIZED (
    SELECT workspace.* FROM "workspace" workspace WHERE workspace.id = $1
),
cleanup_target_workspace_invitation AS MATERIALIZED (
    SELECT child.* FROM "workspace_invitation" child WHERE EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id")
),
cleanup_delete_activity_log AS (
    DELETE FROM "activity_log" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_activity_log)
),
cleanup_delete_agent AS (
    DELETE FROM "agent" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent)
),
cleanup_delete_agent_run AS (
    DELETE FROM "agent_run" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run)
),
cleanup_delete_agent_run_message AS (
    DELETE FROM "agent_run_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_message)
),
cleanup_delete_agent_run_token AS (
    DELETE FROM "agent_run_token" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_token)
),
cleanup_delete_agent_run_usage AS (
    DELETE FROM "agent_run_usage" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_run_usage)
),
cleanup_delete_agent_runtime AS (
    DELETE FROM "agent_runtime" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_agent_runtime)
),
cleanup_delete_agent_skill AS (
    DELETE FROM "agent_skill" child WHERE (child."agent_id", child."skill_id") IN (SELECT "agent_id", "skill_id" FROM cleanup_target_agent_skill)
),
cleanup_delete_attachment AS (
    DELETE FROM "attachment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_attachment)
),
cleanup_delete_autopilot AS (
    DELETE FROM "autopilot" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_autopilot)
),
cleanup_delete_autopilot_run AS (
    DELETE FROM "autopilot_run" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_autopilot_run)
),
cleanup_delete_autopilot_trigger AS (
    DELETE FROM "autopilot_trigger" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_autopilot_trigger)
),
cleanup_delete_chat_message AS (
    DELETE FROM "chat_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_message)
),
cleanup_delete_chat_session AS (
    DELETE FROM "chat_session" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_chat_session)
),
cleanup_delete_comment AS (
    DELETE FROM "comment" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_comment)
),
cleanup_delete_comment_reaction AS (
    DELETE FROM "comment_reaction" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_comment_reaction)
),
cleanup_delete_daemon_connection AS (
    DELETE FROM "daemon_connection" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_daemon_connection)
),
cleanup_delete_daemon_token AS (
    DELETE FROM "daemon_token" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_daemon_token)
),
cleanup_delete_github_installation AS (
    DELETE FROM "github_installation" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_github_installation)
),
cleanup_delete_github_pull_request AS (
    DELETE FROM "github_pull_request" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_github_pull_request)
),
cleanup_delete_github_pull_request_check_suite AS (
    DELETE FROM "github_pull_request_check_suite" child WHERE (child."pr_id", child."suite_id") IN (SELECT "pr_id", "suite_id" FROM cleanup_target_github_pull_request_check_suite)
),
cleanup_delete_inbox_item AS (
    DELETE FROM "inbox_item" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_inbox_item)
),
cleanup_delete_lark_binding_token AS (
    DELETE FROM "lark_binding_token" child WHERE (child."token_hash") IN (SELECT "token_hash" FROM cleanup_target_lark_binding_token)
),
cleanup_delete_lark_chat_session_binding AS (
    DELETE FROM "lark_chat_session_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_chat_session_binding)
),
cleanup_delete_lark_inbound_message_dedup AS (
    DELETE FROM "lark_inbound_message_dedup" child WHERE (child."installation_id", child."message_id") IN (SELECT "installation_id", "message_id" FROM cleanup_target_lark_inbound_message_dedup)
),
cleanup_delete_lark_installation AS (
    DELETE FROM "lark_installation" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_installation)
),
cleanup_delete_lark_outbound_card_message AS (
    DELETE FROM "lark_outbound_card_message" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message)
),
cleanup_delete_lark_user_binding AS (
    DELETE FROM "lark_user_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_user_binding)
),
cleanup_delete_member AS (
    DELETE FROM "member" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_member)
),
cleanup_delete_notification_preference AS (
    DELETE FROM "notification_preference" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_notification_preference)
),
cleanup_delete_pinned_item AS (
    DELETE FROM "pinned_item" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_pinned_item)
),
cleanup_delete_project AS (
    DELETE FROM "project" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_project)
),
cleanup_delete_project_resource AS (
    DELETE FROM "project_resource" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_project_resource)
),
cleanup_delete_skill AS (
    DELETE FROM "skill" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_skill)
),
cleanup_delete_skill_file AS (
    DELETE FROM "skill_file" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_skill_file)
),
cleanup_delete_squad AS (
    DELETE FROM "squad" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_squad)
),
cleanup_delete_squad_member AS (
    DELETE FROM "squad_member" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_squad_member)
),
cleanup_delete_task AS (
    DELETE FROM "task" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task)
),
cleanup_delete_task_dependency AS (
    DELETE FROM "task_dependency" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task_dependency)
),
cleanup_delete_task_label AS (
    DELETE FROM "task_label" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task_label)
),
cleanup_delete_task_pull_request AS (
    DELETE FROM "task_pull_request" child WHERE (child."task_id", child."pull_request_id") IN (SELECT "task_id", "pull_request_id" FROM cleanup_target_task_pull_request)
),
cleanup_delete_task_reaction AS (
    DELETE FROM "task_reaction" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_task_reaction)
),
cleanup_delete_task_subscriber AS (
    DELETE FROM "task_subscriber" child WHERE (child."task_id", child."user_type", child."user_id") IN (SELECT "task_id", "user_type", "user_id" FROM cleanup_target_task_subscriber)
),
cleanup_delete_task_to_label AS (
    DELETE FROM "task_to_label" child WHERE (child."task_id", child."label_id") IN (SELECT "task_id", "label_id" FROM cleanup_target_task_to_label)
),
cleanup_delete_webhook_delivery AS (
    DELETE FROM "webhook_delivery" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_webhook_delivery)
),
cleanup_delete_workspace_invitation AS (
    DELETE FROM "workspace_invitation" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_workspace_invitation)
),
cleanup_detach_agent_run AS (
    UPDATE "agent_run" child SET "trigger_comment_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."trigger_comment_id" = parent."id") THEN NULL ELSE child."trigger_comment_id" END,
        "chat_session_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") THEN NULL ELSE child."chat_session_id" END,
        "autopilot_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_autopilot_run parent WHERE child."autopilot_run_id" = parent."id") THEN NULL ELSE child."autopilot_run_id" END,
        "parent_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id") THEN NULL ELSE child."parent_run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_comment parent WHERE child."trigger_comment_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_chat_session parent WHERE child."chat_session_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_autopilot_run parent WHERE child."autopilot_run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."parent_run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_agent_run))
),
cleanup_detach_autopilot AS (
    UPDATE "autopilot" child SET "project_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_project parent WHERE child."project_id" = parent."id") THEN NULL ELSE child."project_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_project parent WHERE child."project_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_autopilot))
),
cleanup_detach_autopilot_run AS (
    UPDATE "autopilot_run" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END,
        "trigger_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id") THEN NULL ELSE child."trigger_id" END,
        "task_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") THEN NULL ELSE child."task_id" END,
        "squad_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id") THEN NULL ELSE child."squad_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_autopilot_trigger parent WHERE child."trigger_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_squad parent WHERE child."squad_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_autopilot_run))
),
cleanup_detach_chat_session AS (
    UPDATE "chat_session" child SET "runtime_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id") THEN NULL ELSE child."runtime_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_runtime parent WHERE child."runtime_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_chat_session))
),
cleanup_detach_feedback AS (
    UPDATE "feedback" child SET "workspace_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id") THEN NULL ELSE child."workspace_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_workspace parent WHERE child."workspace_id" = parent."id"))
),
cleanup_detach_lark_inbound_audit AS (
    UPDATE "lark_inbound_audit" child SET "installation_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id") THEN NULL ELSE child."installation_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_lark_installation parent WHERE child."installation_id" = parent."id"))
),
cleanup_detach_lark_outbound_card_message AS (
    UPDATE "lark_outbound_card_message" child SET "run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id") THEN NULL ELSE child."run_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_agent_run parent WHERE child."run_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_lark_outbound_card_message))
),
cleanup_detach_task AS (
    UPDATE "task" child SET "parent_task_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."parent_task_id" = parent."id") THEN NULL ELSE child."parent_task_id" END,
        "project_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_project parent WHERE child."project_id" = parent."id") THEN NULL ELSE child."project_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_task parent WHERE child."parent_task_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_project parent WHERE child."project_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_task))
),
cleanup_detach_webhook_delivery AS (
    UPDATE "webhook_delivery" child SET "autopilot_run_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_autopilot_run parent WHERE child."autopilot_run_id" = parent."id") THEN NULL ELSE child."autopilot_run_id" END,
        "replayed_from_delivery_id" = CASE WHEN EXISTS (SELECT 1 FROM cleanup_target_webhook_delivery parent WHERE child."replayed_from_delivery_id" = parent."id") THEN NULL ELSE child."replayed_from_delivery_id" END
    WHERE (EXISTS (SELECT 1 FROM cleanup_target_autopilot_run parent WHERE child."autopilot_run_id" = parent."id") OR EXISTS (SELECT 1 FROM cleanup_target_webhook_delivery parent WHERE child."replayed_from_delivery_id" = parent."id")) AND NOT ((child."id") IN (SELECT "id" FROM cleanup_target_webhook_delivery))
)
DELETE FROM "workspace" workspace WHERE (workspace."id") IN (SELECT "id" FROM cleanup_target_workspace) ;
