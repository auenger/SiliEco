-- name: ListWorkflows :many
SELECT * FROM workflow
WHERE workspace_id = $1
  AND (
    sqlc.narg('project_id')::uuid IS NULL
    OR project_id IS NULL
    OR project_id = sqlc.narg('project_id')
  )
  AND (sqlc.arg('include_archived')::boolean OR status <> 'archived')
ORDER BY updated_at DESC, created_at DESC;

-- name: GetWorkflowInWorkspace :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWorkflow :one
INSERT INTO workflow (
    workspace_id, project_id, name, description, status, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateWorkflowMetadata :one
UPDATE workflow SET
    name = COALESCE(sqlc.narg('name'), name),
    description = sqlc.narg('description'),
    project_id = COALESCE(sqlc.narg('project_id'), project_id),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CountWorkflowInstances :one
SELECT COUNT(*) FROM workflow_instance
WHERE workflow_id = $1 AND workspace_id = $2;

-- name: SetWorkflowCurrentVersion :one
UPDATE workflow SET
    current_version_id = $3,
    status = $4,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveWorkflow :one
UPDATE workflow SET
    status = 'archived',
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: NextWorkflowVersionNumber :one
SELECT COALESCE(MAX(version), 0)::integer + 1
FROM workflow_version
WHERE workflow_id = $1 AND workspace_id = $2;

-- name: LockWorkflowForVersion :one
SELECT * FROM workflow
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: ListWorkflowVersions :many
SELECT * FROM workflow_version
WHERE workflow_id = $1 AND workspace_id = $2
ORDER BY version DESC;

-- name: GetWorkflowVersionInWorkspace :one
SELECT * FROM workflow_version
WHERE id = $1 AND workflow_id = $2 AND workspace_id = $3;

-- name: CreateWorkflowVersion :one
INSERT INTO workflow_version (
    workspace_id, workflow_id, version, status, created_by
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: PublishWorkflowVersion :one
UPDATE workflow_version SET
    status = 'published',
    published_at = COALESCE(published_at, now())
WHERE id = $1 AND workflow_id = $2 AND workspace_id = $3
RETURNING *;

-- name: SupersedeOtherWorkflowVersions :exec
UPDATE workflow_version SET
    status = 'superseded'
WHERE workflow_id = $1
  AND workspace_id = $2
  AND id <> $3
  AND status = 'published';

-- name: ListWorkflowStages :many
SELECT * FROM workflow_stage
WHERE workflow_version_id = $1 AND workspace_id = $2
ORDER BY position ASC;

-- name: GetWorkflowStageInVersion :one
SELECT * FROM workflow_stage
WHERE id = $1
  AND workflow_version_id = $2
  AND workspace_id = $3;

-- name: GetWorkflowStageByStableKey :one
SELECT * FROM workflow_stage
WHERE workflow_version_id = $1
  AND workspace_id = $2
  AND stable_key = $3;

-- name: GetFirstWorkflowStage :one
SELECT * FROM workflow_stage
WHERE workflow_version_id = $1 AND workspace_id = $2
ORDER BY position ASC
LIMIT 1;

-- name: GetNextWorkflowStage :one
SELECT * FROM workflow_stage
WHERE workflow_version_id = $1
  AND workspace_id = $2
  AND position > $3
ORDER BY position ASC
LIMIT 1;

-- name: CreateWorkflowStage :one
INSERT INTO workflow_stage (
    workspace_id, workflow_version_id, stable_key, name, description,
    position, completion_rule, input_spec, output_spec, required_skills,
    gate, rollback_stage_key, allowed_mcp_server_ids, execution_mode, executor_id, input_bindings, rework_policy, retry_policy
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, COALESCE(sqlc.narg('execution_mode')::text, 'manual'), sqlc.narg('executor_id'), COALESCE(sqlc.narg('input_bindings')::jsonb, '{}'::jsonb), COALESCE(sqlc.narg('rework_policy')::jsonb, '{}'::jsonb), COALESCE(sqlc.narg('retry_policy')::jsonb, '{}'::jsonb)
) RETURNING *;

-- name: ListWorkflowInstances :many
SELECT * FROM workflow_instance
WHERE workspace_id = $1
  AND (sqlc.narg('workflow_id')::uuid IS NULL OR workflow_id = sqlc.narg('workflow_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY updated_at DESC, created_at DESC;

-- name: ListWorkflowStageRuns :many
SELECT t.id, t.issue_id, i.title AS task_title, t.agent_id, a.name AS agent_name,
       t.status, t.trigger_summary, t.started_at, t.completed_at, t.result,
       t.error, t.failure_reason, t.execution_manifest, t.originator_source,
       t.originator_user_id, t.accountable_user_id, t.created_at,
       t.worktree_branch, t.worktree_base_sha, t.worktree_final_sha,
       t.worktree_delivery_status
FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
LEFT JOIN agent a ON a.id = t.agent_id
LEFT JOIN workflow_stage_execution e ON e.id = t.workflow_stage_execution_id AND e.workspace_id = i.workspace_id
WHERE i.workspace_id = $1
  AND ((t.workflow_stage_execution_id IS NOT NULL AND e.workflow_instance_id = $2 AND e.workflow_stage_id = $3)
    OR (t.workflow_stage_execution_id IS NULL AND i.workflow_instance_id = $2 AND i.workflow_stage_id = $3))
ORDER BY t.created_at DESC, t.id DESC
LIMIT $4 OFFSET $5;

-- name: CountWorkflowStageRuns :one
SELECT COUNT(*)
FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
LEFT JOIN workflow_stage_execution e ON e.id = t.workflow_stage_execution_id AND e.workspace_id = i.workspace_id
WHERE i.workspace_id = $1
  AND ((t.workflow_stage_execution_id IS NOT NULL AND e.workflow_instance_id = $2 AND e.workflow_stage_id = $3)
    OR (t.workflow_stage_execution_id IS NULL AND i.workflow_instance_id = $2 AND i.workflow_stage_id = $3));

-- name: CountIssuesInWorkflowInstance :one
SELECT count(*)::bigint
FROM issue
WHERE workspace_id = $1
  AND workflow_instance_id = $2;

-- name: ArchiveWorkflowInstance :one
UPDATE workflow_instance SET
    archived_at = now(),
    archived_by = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND archived_at IS NULL
RETURNING *;

-- name: GetWorkflowInstanceInWorkspace :one
SELECT * FROM workflow_instance
WHERE id = $1 AND workspace_id = $2;

-- name: LockWorkflowInstanceInWorkspace :one
SELECT * FROM workflow_instance
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateWorkflowInstance :one
INSERT INTO workflow_instance (
    workspace_id, workflow_id, workflow_version_id, title, description,
    status, current_stage_id, project_id, created_by, started_at, inputs
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    CASE WHEN $6 = 'active' THEN now() ELSE NULL END, COALESCE(sqlc.narg('inputs')::jsonb, '{}'::jsonb)
) RETURNING *;

-- name: UpdateWorkflowInstanceState :one
UPDATE workflow_instance SET
    status = COALESCE(sqlc.narg('status'), status),
    current_stage_id = COALESCE(sqlc.narg('current_stage_id'), current_stage_id),
    started_at = CASE
        WHEN sqlc.narg('status')::text = 'active' THEN COALESCE(started_at, now())
        ELSE started_at
    END,
    completed_at = CASE
        WHEN sqlc.narg('status')::text = 'completed' THEN COALESCE(completed_at, now())
        WHEN sqlc.narg('status')::text IS NOT NULL AND sqlc.narg('status')::text <> 'completed' THEN NULL
        ELSE completed_at
    END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: UpdateWorkflowInstancePlanMetadata :one
UPDATE workflow_instance SET
    title = $3,
    description = $4,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND revision = $5
RETURNING *;

-- name: ListWorkflowInstanceStages :many
SELECT * FROM workflow_instance_stage
WHERE workflow_instance_id = $1 AND workspace_id = $2
ORDER BY position ASC;

-- name: GetWorkflowInstanceStage :one
SELECT * FROM workflow_instance_stage
WHERE id = $1
  AND workflow_instance_id = $2
  AND workspace_id = $3;

-- name: GetFirstWorkflowInstanceStage :one
SELECT * FROM workflow_instance_stage
WHERE workflow_instance_id = $1 AND workspace_id = $2
ORDER BY position ASC
LIMIT 1;

-- name: GetNextWorkflowInstanceStage :one
SELECT * FROM workflow_instance_stage
WHERE workflow_instance_id = $1
  AND workspace_id = $2
  AND position > $3
ORDER BY position ASC
LIMIT 1;

-- name: GetWorkflowInstanceStageByStableKey :one
SELECT * FROM workflow_instance_stage
WHERE workflow_instance_id = $1
  AND workspace_id = $2
  AND stable_key = $3;

-- name: CreateWorkflowInstanceStage :one
INSERT INTO workflow_instance_stage (
    workspace_id, workflow_instance_id, source_stage_id, stable_key, name,
    description, position, completion_rule, input_spec, output_spec,
    required_skills, required_skill_snapshots, gate, rollback_stage_key,
    allowed_mcp_server_ids, execution_mode, executor_id, input_bindings, rework_policy, retry_policy
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, COALESCE(sqlc.narg('execution_mode')::text, 'manual'), sqlc.narg('executor_id'), COALESCE(sqlc.narg('input_bindings')::jsonb, '{}'::jsonb), COALESCE(sqlc.narg('rework_policy')::jsonb, '{}'::jsonb), COALESCE(sqlc.narg('retry_policy')::jsonb, '{}'::jsonb)
) RETURNING *;

-- name: ShiftWorkflowInstanceStagePositions :exec
UPDATE workflow_instance_stage
SET position = position + 1000,
    updated_at = now()
WHERE workflow_instance_id = $1 AND workspace_id = $2;

-- name: UpdateWorkflowInstanceStage :one
UPDATE workflow_instance_stage SET
    stable_key = $4,
    name = $5,
    description = $6,
    position = $7,
    completion_rule = $8,
    input_spec = $9,
    output_spec = $10,
    required_skills = $11,
    required_skill_snapshots = $12,
    gate = $13,
    rollback_stage_key = $14,
    allowed_mcp_server_ids = $15,
    execution_mode = COALESCE(sqlc.narg('execution_mode')::text, 'manual'),
    executor_id = sqlc.narg('executor_id'),
    input_bindings = COALESCE(sqlc.narg('input_bindings')::jsonb, '{}'::jsonb),
    rework_policy = COALESCE(sqlc.narg('rework_policy')::jsonb, '{}'::jsonb),
    retry_policy = COALESCE(sqlc.narg('retry_policy')::jsonb, '{}'::jsonb),
    updated_at = now()
WHERE id = $1
  AND workflow_instance_id = $2
  AND workspace_id = $3
RETURNING *;

-- name: DeleteWorkflowInstanceStage :execrows
DELETE FROM workflow_instance_stage
WHERE id = $1
  AND workflow_instance_id = $2
  AND workspace_id = $3;

-- name: CreateWorkflowInstanceChange :one
INSERT INTO workflow_instance_change (
    workspace_id, workflow_instance_id, revision, changed_by,
    change_note, before_plan, after_plan
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7
) RETURNING *;

-- name: ListWorkflowInstanceChanges :many
SELECT * FROM workflow_instance_change
WHERE workflow_instance_id = $1 AND workspace_id = $2
ORDER BY revision DESC;

-- name: CreateWorkflowGateDecision :one
INSERT INTO workflow_gate_decision (
    workspace_id, workflow_instance_id, from_stage_id, to_stage_id,
    outcome, actor_type, actor_id, note
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8
) RETURNING *;

-- name: ListWorkflowGateDecisions :many
SELECT * FROM workflow_gate_decision
WHERE workflow_instance_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: ListIssuesForWorkflowInstance :many
SELECT * FROM issue
WHERE workspace_id = $1
  AND workflow_instance_id = $2
ORDER BY workflow_stage_id, position ASC, created_at ASC;

-- name: AttachIssueToWorkflowStage :one
UPDATE issue SET
    workflow_instance_id = $3,
    workflow_stage_id = $4,
    workflow_stage_execution_id = CASE WHEN issue.workflow_instance_id=$3 AND issue.workflow_stage_id=$4 THEN issue.workflow_stage_execution_id ELSE (SELECT e.id FROM workflow_stage_execution e WHERE e.workspace_id=$2 AND e.workflow_instance_id=$3 AND e.workflow_stage_id=$4 ORDER BY e.attempt DESC LIMIT 1) END,
    project_id = $5,
    updated_at = now()
WHERE issue.id = $1 AND issue.workspace_id = $2
RETURNING issue.*;

-- name: DetachIssueFromWorkflow :one
UPDATE issue SET
    workflow_instance_id = NULL,
    workflow_stage_id = NULL,
    workflow_stage_execution_id = NULL,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND workflow_instance_id = $3
RETURNING issue.*;

-- name: LockWorkflowStageIssues :many
-- The transition transaction rechecks delivery after acquiring the Run lock.
SELECT id, status FROM issue
WHERE workspace_id=$1 AND workflow_instance_id=$2 AND workflow_stage_id=$3
AND workflow_invalidated_by_rework_id IS NULL
ORDER BY id FOR UPDATE;

-- name: CountIssuesByWorkflowStage :many
SELECT workflow_stage_id, count(*)::bigint AS task_count
FROM issue
WHERE workspace_id = $1
  AND workflow_instance_id = $2
GROUP BY workflow_stage_id;
