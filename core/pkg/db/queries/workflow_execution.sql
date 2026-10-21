-- name: CreateWorkflowStageExecution :one
INSERT INTO workflow_stage_execution (
    workspace_id, workflow_instance_id, workflow_stage_id, attempt,
    plan_revision, executor_id, config_snapshot, input_snapshot
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetWorkflowStageExecution :one
SELECT * FROM workflow_stage_execution
WHERE workspace_id = $1 AND id = $2;

-- name: GetLatestWorkflowStageExecution :one
SELECT * FROM workflow_stage_execution
WHERE workspace_id = $1 AND workflow_instance_id = $2 AND workflow_stage_id = $3
ORDER BY attempt DESC LIMIT 1;

-- name: ListWorkflowStageExecutions :many
SELECT * FROM workflow_stage_execution
WHERE workspace_id = $1 AND workflow_instance_id = $2
ORDER BY created_at, id;

-- name: CreateWorkflowExecutionAction :one
INSERT INTO workflow_execution_action (workspace_id, workflow_instance_id, execution_id, kind)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ClaimWorkflowExecutionAction :one
-- Each statement is an atomic lease acquisition. Delivery must still lock and
-- revalidate the Run before doing any work; a lease is not execution authority.
WITH candidate AS (
    SELECT action.id FROM workflow_execution_action AS action
    JOIN workflow_stage_execution AS execution
      ON execution.id = action.execution_id AND execution.workspace_id = action.workspace_id
     AND execution.workflow_instance_id = action.workflow_instance_id
    JOIN workflow_instance AS instance
      ON instance.id = action.workflow_instance_id AND instance.workspace_id = action.workspace_id
    WHERE action.workspace_id = sqlc.arg('workspace_id')
      AND instance.status = 'active' AND instance.archived_at IS NULL
      AND instance.current_stage_id = execution.workflow_stage_id
      AND instance.revision = execution.plan_revision
      AND execution.status NOT IN ('succeeded', 'failed', 'cancelled', 'blocked')
      AND action.available_at <= now()
      AND (action.status = 'pending' OR (action.status = 'leased' AND action.lease_until <= now()))
    ORDER BY action.available_at, action.created_at, action.id
    LIMIT 1 FOR UPDATE OF action SKIP LOCKED
)
UPDATE workflow_execution_action AS action
SET status = 'leased', lease_until = now() + INTERVAL '60 seconds',
    claim_token = gen_random_uuid(), attempts = attempts + 1, updated_at = now()
FROM candidate WHERE action.id = candidate.id
RETURNING action.*;

-- name: CompleteWorkflowExecutionAction :execrows
UPDATE workflow_execution_action
SET status = 'delivered', claim_token = NULL, lease_until = NULL, last_error = NULL, updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND claim_token = $3
  AND status = 'leased' AND lease_until > now();

-- name: RetryWorkflowExecutionAction :execrows
UPDATE workflow_execution_action
SET status = 'pending', claim_token = NULL, lease_until = NULL,
    available_at = $4, last_error = $5, updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND claim_token = $3
  AND status = 'leased' AND lease_until > now();

-- name: CancelWorkflowExecutionActions :exec
UPDATE workflow_execution_action
SET status = 'cancelled', claim_token = NULL, lease_until = NULL, updated_at = now()
WHERE workspace_id = $1 AND workflow_instance_id = $2 AND status IN ('pending', 'leased');

-- name: DeleteProjectWorkflowExecutionActions :exec
DELETE FROM workflow_execution_action AS action
WHERE action.workspace_id = sqlc.arg('workspace_id')
  AND workflow_instance_id IN (
    SELECT instance.id FROM workflow_instance AS instance
    WHERE instance.project_id = sqlc.arg('project_id') AND instance.workspace_id = sqlc.arg('workspace_id')
  );

-- name: DeleteProjectWorkflowStageExecutions :exec
DELETE FROM workflow_stage_execution AS execution
WHERE execution.workspace_id = sqlc.arg('workspace_id')
  AND workflow_instance_id IN (
    SELECT instance.id FROM workflow_instance AS instance
    WHERE instance.project_id = sqlc.arg('project_id') AND instance.workspace_id = sqlc.arg('workspace_id')
  );

-- name: LockWorkspaceForWorkflowExecution :one
-- Match workspace deletion's FOR UPDATE barrier before taking any Run lock.
SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE;

-- name: LockProjectForWorkflowExecution :one
-- Project deletion takes FOR UPDATE before sweeping its workflow children.
SELECT id FROM project WHERE id = $1 AND workspace_id = $2 FOR KEY SHARE;

-- name: LockWorkflowExecutionAction :one
SELECT * FROM workflow_execution_action
WHERE workspace_id = $1 AND id = $2 AND claim_token = $3
  AND status = 'leased' AND lease_until > now()
FOR UPDATE;

-- name: BindWorkflowExecutionTask :one
UPDATE agent_task_queue AS task
SET workflow_stage_execution_id = sqlc.arg('execution_id'),
    max_attempts = COALESCE(sqlc.narg('max_attempts')::integer, task.max_attempts)
FROM workflow_stage_execution AS execution
WHERE task.id = sqlc.arg('task_id') AND execution.id = sqlc.arg('execution_id')
  AND execution.workspace_id = sqlc.arg('workspace_id')
  AND task.agent_id = execution.executor_id
  AND task.workflow_stage_execution_id IS NULL AND task.status = 'queued'
RETURNING task.*;

-- name: MarkWorkflowExecutionQueued :one
UPDATE workflow_stage_execution
SET main_issue_id = $3, main_task_id = $4, status = 'queued', updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND status = 'pending'
  AND main_issue_id IS NULL AND main_task_id IS NULL
RETURNING *;

-- name: BlockWorkflowStageExecution :one
UPDATE workflow_stage_execution
SET status = 'blocked', failure_code = $3, failure_detail = $4, updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND status = 'pending'
RETURNING *;

-- name: CancelWorkflowExecutionAction :execrows
UPDATE workflow_execution_action
SET status = 'cancelled', claim_token = NULL, lease_until = NULL, updated_at = now()
WHERE workspace_id = $1 AND id = $2 AND claim_token = $3 AND status = 'leased';

-- name: GetWorkflowExecutionAction :one
SELECT * FROM workflow_execution_action WHERE workspace_id = $1 AND id = $2;

-- name: ListWorkflowDispatchWorkspaces :many
-- System worker scope discovery; each subsequent claim remains workspace-bound.
SELECT DISTINCT action.workspace_id FROM workflow_execution_action AS action
JOIN workflow_stage_execution AS execution ON execution.id = action.execution_id AND execution.workspace_id = action.workspace_id
JOIN workflow_instance AS instance ON instance.id = action.workflow_instance_id AND instance.workspace_id = action.workspace_id
WHERE action.kind = 'dispatch' AND action.available_at <= now()
  AND (action.status = 'pending' OR (action.status = 'leased' AND action.lease_until <= now()))
  AND instance.status = 'active' AND instance.archived_at IS NULL
  AND instance.current_stage_id = execution.workflow_stage_id AND instance.revision = execution.plan_revision
  AND execution.status = 'pending'
ORDER BY action.workspace_id;

-- name: LockIssueForWorkflowMutation :one
SELECT * FROM issue WHERE id=$1 AND workspace_id=$2 FOR UPDATE;

-- name: AcceptWorkflowStageExecution :one
UPDATE workflow_stage_execution
SET status='succeeded', acceptance=$3, finished_at=now(), updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND status='awaiting_review' AND result_revision=$4 AND acceptance IS NULL
RETURNING *;

-- name: ListWorkflowAutomaticAcceptanceCandidates :many
SELECT e.workspace_id, e.workflow_instance_id, e.id
FROM workflow_stage_execution e
JOIN workflow_instance r ON r.id=e.workflow_instance_id AND r.workspace_id=e.workspace_id
WHERE e.status='awaiting_review' AND e.config_snapshot->'gate'->>'type'='none'
  AND r.status='active' AND r.archived_at IS NULL
  AND r.current_stage_id=e.workflow_stage_id AND r.revision=e.plan_revision
  AND e.id > sqlc.arg(after_id)
ORDER BY e.id LIMIT 100;

-- name: ResumeWorkflowInputBlockedExecution :one
UPDATE workflow_stage_execution
SET status='pending', input_snapshot=$3, failure_code=NULL, failure_detail=NULL, updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND status='blocked' AND failure_code='input_unavailable'
  AND main_issue_id IS NULL AND main_task_id IS NULL AND result_revision=0
RETURNING *;

-- name: CorrectWorkflowRunInputs :one
UPDATE workflow_instance SET inputs=sqlc.arg(inputs),updated_at=now()
WHERE workspace_id=sqlc.arg(workspace_id) AND id=sqlc.arg(id)
 AND inputs=sqlc.arg(expected_inputs)::jsonb AND status='active' AND archived_at IS NULL
RETURNING *;

-- name: ListWorkflowRunningDeadlineCalls :many
SELECT task.id, task.started_at, execution.config_snapshot
FROM agent_task_queue task
JOIN agent ON agent.id=task.agent_id
LEFT JOIN workflow_review_request request ON request.id=task.workflow_review_request_id AND request.workspace_id=agent.workspace_id
JOIN workflow_stage_execution execution ON execution.id=COALESCE(task.workflow_stage_execution_id,request.execution_id) AND execution.workspace_id=agent.workspace_id
WHERE task.status='running' AND task.started_at IS NOT NULL
  AND task.id > sqlc.arg(after_id)
  AND execution.config_snapshot->'retry_policy'->'timeout_seconds' IS NOT NULL
ORDER BY task.id LIMIT 100;
