-- name: GetWorkflowExecutionRework :one
SELECT * FROM workflow_stage_execution WHERE workspace_id=$1 AND rework_source_execution_id=$2;

-- name: MarkWorkflowExecutionRework :one
UPDATE workflow_stage_execution SET rework_source_execution_id=$3,rework_metadata=$4
WHERE workspace_id=$1 AND id=$2 AND rework_source_execution_id IS NULL RETURNING *;

-- name: ListWorkflowReworkExecutions :many
SELECT e.* FROM workflow_stage_execution e
JOIN workflow_instance_stage stage ON stage.id=e.workflow_stage_id AND stage.workflow_instance_id=e.workflow_instance_id AND stage.workspace_id=e.workspace_id
WHERE e.workspace_id=$1 AND e.workflow_instance_id=$2 AND stage.position >= $3
AND e.attempt=(SELECT max(latest.attempt) FROM workflow_stage_execution latest WHERE latest.workspace_id=e.workspace_id AND latest.workflow_instance_id=e.workflow_instance_id AND latest.workflow_stage_id=e.workflow_stage_id)
ORDER BY stage.position,e.id FOR UPDATE OF e;

-- name: InvalidateWorkflowExecutionForRework :exec
UPDATE workflow_stage_execution SET status='cancelled',failure_code='rework_invalidated',failure_detail=$3,updated_at=now(),finished_at=COALESCE(finished_at,now())
WHERE workspace_id=$1 AND id=$2;

-- name: CancelWorkflowActionsForRework :exec
UPDATE workflow_execution_action SET status='cancelled',claim_token=NULL,lease_until=NULL,updated_at=now()
WHERE workspace_id=$1 AND execution_id=$2 AND status IN ('pending','leased');

-- name: InvalidateWorkflowReviewsForRework :exec
UPDATE workflow_review_request SET status='invalidated',updated_at=now()
WHERE workspace_id=$1 AND execution_id=$2 AND status<>'invalidated';

-- name: ListWorkflowCallsForRework :many
SELECT task.* FROM agent_task_queue task
WHERE task.workflow_stage_execution_id=sqlc.arg(execution_id)
OR EXISTS (SELECT 1 FROM workflow_review_request r WHERE r.id=task.workflow_review_request_id AND r.workspace_id=sqlc.arg(workspace_id) AND r.execution_id=sqlc.arg(execution_id))
OR EXISTS (SELECT 1 FROM issue i WHERE i.id=task.issue_id AND i.workspace_id=sqlc.arg(workspace_id) AND i.workflow_stage_execution_id=sqlc.arg(execution_id))
ORDER BY task.id FOR UPDATE;

-- name: BindLegacyIssuesForRework :exec
UPDATE issue i SET workflow_stage_execution_id=e.id
FROM workflow_stage_execution e
WHERE e.workspace_id=$1 AND e.id=$2 AND i.workspace_id=e.workspace_id
AND i.workflow_instance_id=e.workflow_instance_id AND i.workflow_stage_id=e.workflow_stage_id
AND i.workflow_stage_execution_id IS NULL;

-- name: InvalidateWorkflowStageIssuesForRework :exec
-- Lock and invalidate issues before reading their calls. Retry inserts take a
-- shared issue lock, so a concurrent child is either cancelled or refused.
UPDATE issue i SET workflow_invalidated_by_rework_id=sqlc.arg(rework_id),updated_at=now()
FROM workflow_instance_stage stage
WHERE i.workspace_id=sqlc.arg(workspace_id) AND i.workflow_instance_id=sqlc.arg(instance_id)
AND stage.workspace_id=i.workspace_id AND stage.workflow_instance_id=i.workflow_instance_id AND stage.id=i.workflow_stage_id
AND stage.position >= sqlc.arg(position) AND i.workflow_invalidated_by_rework_id IS NULL;

-- name: ListInvalidatedWorkflowCallsForRework :many
SELECT task.* FROM agent_task_queue task
JOIN issue i ON i.id=task.issue_id
WHERE i.workspace_id=$1 AND i.workflow_instance_id=$2 AND i.workflow_invalidated_by_rework_id=$3
ORDER BY task.id FOR UPDATE OF task;

-- name: InvalidateWorkflowStageDecisionsForRework :exec
UPDATE workflow_gate_decision decision SET invalidated_by_rework_id=sqlc.arg(rework_id)
FROM workflow_instance_stage stage
WHERE decision.workspace_id=sqlc.arg(workspace_id) AND decision.workflow_instance_id=sqlc.arg(instance_id)
AND stage.workspace_id=decision.workspace_id AND stage.workflow_instance_id=decision.workflow_instance_id AND stage.id=decision.from_stage_id
AND stage.position >= sqlc.arg(position) AND decision.invalidated_by_rework_id IS NULL;

-- name: CreateWorkflowRework :one
INSERT INTO workflow_rework(workspace_id,workflow_instance_id,source_stage_id,target_stage_id,source_execution_id,source_state_token,plan_revision,actor_type,actor_id,reason)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetWorkflowReworkByExecution :one
SELECT * FROM workflow_rework WHERE workspace_id=$1 AND source_execution_id=$2;

-- name: ListWorkflowReworks :many
SELECT * FROM workflow_rework WHERE workspace_id=$1 AND workflow_instance_id=$2 ORDER BY created_at,id;

-- name: CountWorkflowStageReworks :one
SELECT count(*) FROM workflow_rework WHERE workspace_id=$1 AND workflow_instance_id=$2 AND source_stage_id=$3;
