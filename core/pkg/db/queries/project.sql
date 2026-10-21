-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
ORDER BY created_at DESC;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: LockProjectForChatSessionCreate :one
-- Conflicts with project deletion so a chat session cannot commit a soft
-- project reference after the delete transaction has swept existing sessions.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockProjectForDelete :one
-- Serializes project deletion with chat-session creation. The handler locks,
-- clears every soft chat reference, and deletes the project in one transaction.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: ClearProjectWorkflowTasks :exec
UPDATE issue SET
    workflow_instance_id = NULL,
    workflow_stage_id = NULL,
    workflow_stage_execution_id = NULL,
    workflow_invalidated_by_rework_id = NULL,
    updated_at = now()
WHERE project_id = sqlc.arg('project_id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: DeleteProjectWorkflowGateDecisions :exec
WITH cleared_review_recoveries AS (
 DELETE FROM workflow_review_recovery WHERE workspace_id=sqlc.arg('workspace_id')
 AND workflow_instance_id IN (SELECT id FROM workflow_instance WHERE project_id=sqlc.arg('project_id') AND workspace_id=sqlc.arg('workspace_id'))
), cleared_reworks AS (
 DELETE FROM workflow_rework WHERE workspace_id=sqlc.arg('workspace_id')
 AND workflow_instance_id IN (SELECT id FROM workflow_instance WHERE project_id=sqlc.arg('project_id') AND workspace_id=sqlc.arg('workspace_id'))
), cleared_requests AS (
 DELETE FROM workflow_review_request WHERE workspace_id=sqlc.arg('workspace_id')
 AND workflow_instance_id IN (SELECT id FROM workflow_instance WHERE project_id=sqlc.arg('project_id') AND workspace_id=sqlc.arg('workspace_id'))
)
DELETE FROM workflow_gate_decision AS decision
WHERE decision.workspace_id = sqlc.arg('workspace_id')
  AND workflow_instance_id IN (
      SELECT instance.id FROM workflow_instance AS instance
      WHERE instance.project_id = sqlc.arg('project_id')
        AND instance.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflowInstanceChanges :exec
DELETE FROM workflow_instance_change AS change
WHERE change.workspace_id = sqlc.arg('workspace_id')
  AND workflow_instance_id IN (
      SELECT instance.id FROM workflow_instance AS instance
      WHERE instance.project_id = sqlc.arg('project_id')
        AND instance.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflowInstanceStages :exec
DELETE FROM workflow_instance_stage AS stage
WHERE stage.workspace_id = sqlc.arg('workspace_id')
  AND workflow_instance_id IN (
      SELECT instance.id FROM workflow_instance AS instance
      WHERE instance.project_id = sqlc.arg('project_id')
        AND instance.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflowInstances :exec
DELETE FROM workflow_instance
WHERE project_id = sqlc.arg('project_id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: DeleteProjectWorkflowStages :exec
DELETE FROM workflow_stage AS stage
WHERE stage.workspace_id = sqlc.arg('workspace_id')
  AND workflow_version_id IN (
      SELECT workflow_version.id
      FROM workflow_version
      JOIN workflow ON workflow.id = workflow_version.workflow_id
      WHERE workflow.project_id = sqlc.arg('project_id')
        AND workflow.workspace_id = sqlc.arg('workspace_id')
        AND workflow_version.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflowVersions :exec
DELETE FROM workflow_version AS version
WHERE version.workspace_id = sqlc.arg('workspace_id')
  AND workflow_id IN (
      SELECT workflow.id FROM workflow
      WHERE workflow.project_id = sqlc.arg('project_id')
        AND workflow.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflows :exec
DELETE FROM workflow
WHERE project_id = sqlc.arg('project_id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;
