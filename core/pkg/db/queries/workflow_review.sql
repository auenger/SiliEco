-- name: LockWorkflowReviewIssues :many
SELECT * FROM issue WHERE workspace_id=$1 AND workflow_instance_id=$2 AND workflow_stage_id=$3
AND workflow_invalidated_by_rework_id IS NULL
AND (workflow_stage_execution_id IS NULL OR workflow_stage_execution_id=$4)
ORDER BY id FOR UPDATE;

-- name: LockWorkflowReviewCalls :many
-- Lock all calls that affect the completion barrier, including historical bound
-- calls whose issue was moved. Insertions serialize on the parent Run lock.
SELECT task.id, task.agent_id, task.issue_id, task.status, task.attempt,
       task.workflow_stage_execution_id, task.started_at, task.completed_at, task.created_at
FROM agent_task_queue task
WHERE task.workflow_review_request_id IS NULL AND ((task.workflow_stage_execution_id=sqlc.arg(execution_id) AND EXISTS (
    SELECT 1 FROM workflow_stage_execution execution WHERE execution.id=task.workflow_stage_execution_id
    AND execution.workspace_id=sqlc.arg(workspace_id) AND execution.workflow_instance_id=sqlc.arg(instance_id)))
OR (task.workflow_stage_execution_id IS NULL AND EXISTS (
    SELECT 1 FROM issue WHERE issue.id=task.issue_id AND issue.workspace_id=sqlc.arg(workspace_id)
    AND issue.workflow_instance_id=sqlc.arg(instance_id) AND issue.workflow_stage_id=sqlc.arg(stage_id)
    AND issue.workflow_invalidated_by_rework_id IS NULL
    AND (issue.workflow_stage_execution_id IS NULL OR issue.workflow_stage_execution_id=sqlc.arg(execution_id)))))
ORDER BY task.id FOR UPDATE OF task;

-- name: CreateWorkflowExecutionDecision :one
INSERT INTO workflow_gate_decision (
    workspace_id, workflow_instance_id, from_stage_id, outcome, actor_type, actor_id, note,
    execution_id, result_revision, review_step, basis_hash, basis_snapshot
) VALUES ($1,$2,$3,$4,'member',$5,$6,$7,$8,'human',$9,$10)
RETURNING *;

-- name: GetWorkflowExecutionDecisionByBasis :one
SELECT * FROM workflow_gate_decision
WHERE workspace_id=$1 AND execution_id=$2 AND basis_hash=$3 AND review_step='human';

-- name: CreateWorkflowReviewRequest :one
INSERT INTO workflow_review_request(workspace_id,workflow_instance_id,execution_id,reviewer_id,result_id,result_revision,basis_hash,basis_snapshot)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetWorkflowReviewRequestByBasis :one
SELECT * FROM workflow_review_request WHERE workspace_id=$1 AND execution_id=$2 AND basis_hash=$3;

-- name: InvalidateWorkflowReviewRequests :exec
UPDATE workflow_review_request SET status='invalidated',updated_at=now()
WHERE workspace_id=$1 AND execution_id=$2 AND basis_hash<>$3 AND status IN ('pending','queued');

-- name: BindWorkflowReviewCall :one
-- Caller holds the parent Run lock and revalidates current request evidence.
WITH bound_call AS (
 UPDATE agent_task_queue task SET workflow_review_request_id=request.id, max_attempts=COALESCE(sqlc.narg(max_attempts)::integer,task.max_attempts)
 FROM workflow_review_request request
 JOIN workflow_stage_execution execution ON execution.id=request.execution_id AND execution.workspace_id=request.workspace_id
 WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
 AND request.status='pending' AND request.task_id IS NULL
 AND task.id=sqlc.arg(task_id) AND task.status='queued' AND task.agent_id=request.reviewer_id
 AND task.issue_id=execution.main_issue_id
 AND task.workflow_stage_execution_id IS NULL AND task.workflow_review_request_id IS NULL
 RETURNING task.id
)
UPDATE workflow_review_request request SET task_id=bound_call.id,status='queued',updated_at=now()
FROM bound_call WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
RETURNING request.*;

-- name: GetWorkflowReviewRequest :one
SELECT * FROM workflow_review_request WHERE workspace_id=$1 AND id=$2;

-- name: CreateWorkflowAgentDecision :one
INSERT INTO workflow_gate_decision(workspace_id,workflow_instance_id,from_stage_id,outcome,actor_type,actor_id,note,execution_id,result_revision,review_step,basis_hash,basis_snapshot,review_task_id)
VALUES($1,$2,$3,$4,'agent',$5,$6,$7,$8,'agent',$9,$10,$11) RETURNING *;

-- name: GetWorkflowAgentDecisionByBasis :one
SELECT * FROM workflow_gate_decision WHERE workspace_id=$1 AND execution_id=$2 AND basis_hash=$3 AND review_step='agent' AND review_task_id=$4;

-- name: MarkWorkflowReviewDecided :one
UPDATE workflow_review_request SET status='decided',updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND status='queued' AND task_id=$3 RETURNING *;

-- name: ListQueuedWorkflowReviewsForAgent :many
SELECT request.* FROM workflow_review_request request
JOIN agent_task_queue task ON task.id=request.task_id AND task.workflow_review_request_id=request.id
WHERE request.reviewer_id=$1 AND request.status='queued' AND task.status='queued'
ORDER BY task.priority DESC,task.created_at,request.id LIMIT 100;

-- name: ClaimWorkflowReviewCall :one
UPDATE agent_task_queue task SET status='dispatched',dispatched_at=now(),
 prepare_lease_expires_at=now()+make_interval(secs=>sqlc.arg(prepare_lease_secs)::double precision)
WHERE task.id=sqlc.arg(task_id) AND task.workflow_review_request_id=sqlc.arg(request_id)
 AND task.status='queued'
 AND NOT EXISTS(SELECT 1 FROM agent_task_queue active WHERE active.agent_id=task.agent_id
 AND active.issue_id=task.issue_id AND active.status IN ('dispatched','running','waiting_local_directory'))
RETURNING task.*;

-- name: ListWorkflowAgentReviewCandidates :many
SELECT e.workspace_id, e.workflow_instance_id, e.id,
       COALESCE(e.config_snapshot->'gate'->>'type', '')::text AS gate_type
FROM workflow_stage_execution e
JOIN workflow_instance r ON r.id=e.workflow_instance_id AND r.workspace_id=e.workspace_id
WHERE e.status='awaiting_review' AND e.config_snapshot->'gate'->>'type' IN ('agent','hybrid')
  AND r.status='active' AND r.archived_at IS NULL
  AND r.current_stage_id=e.workflow_stage_id AND r.revision=e.plan_revision
  AND e.id > sqlc.arg(after_id)
ORDER BY e.id LIMIT 100;

-- name: RetryWorkflowReviewCall :one
-- The caller holds the Run lock and rechecks evidence, permissions and budget.
WITH child AS (
 INSERT INTO agent_task_queue (
  agent_id,runtime_id,issue_id,status,priority,trigger_comment_id,coalesced_comment_ids,
  trigger_summary,context,work_dir,attempt,max_attempts,parent_task_id,retry_of_task_id,
  force_fresh_session,handoff_note,is_leader_task,squad_id,originator_user_id,accountable_user_id,
  runtime_mcp_overlay,runtime_connected_apps,originator_source,delegated_from_task_id,rule_version_id,
  trigger_evidence_kind,trigger_evidence_ref_id,workflow_review_request_id
 )
 SELECT p.agent_id,sqlc.arg(runtime_id)::uuid,p.issue_id,'queued',p.priority,p.trigger_comment_id,p.coalesced_comment_ids,
  p.trigger_summary,p.context,CASE WHEN p.runtime_id=sqlc.arg(runtime_id)::uuid THEN p.work_dir ELSE NULL END,p.attempt+1,sqlc.arg(max_attempts),p.id,p.id,
  true,p.handoff_note,p.is_leader_task,p.squad_id,p.originator_user_id,p.accountable_user_id,
  sqlc.arg(runtime_mcp_overlay),sqlc.arg(runtime_connected_apps),p.originator_source,p.delegated_from_task_id,p.rule_version_id,
  p.trigger_evidence_kind,p.trigger_evidence_ref_id,request.id
 FROM workflow_review_request request JOIN agent_task_queue p ON p.id=request.task_id AND p.workflow_review_request_id=request.id
 WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
 AND request.task_id=sqlc.arg(parent_task_id) AND request.status IN ('queued','decided')
 AND (p.status='failed' OR (p.status='completed' AND NOT EXISTS (SELECT 1 FROM workflow_gate_decision d WHERE d.workspace_id=request.workspace_id AND d.execution_id=request.execution_id AND d.basis_hash=request.basis_hash AND d.review_task_id=p.id AND d.review_step='agent'))) AND p.attempt<sqlc.arg(max_attempts) AND p.workflow_stage_execution_id IS NULL
 AND p.agent_id=request.reviewer_id AND EXISTS (SELECT 1 FROM workflow_stage_execution e WHERE e.id=request.execution_id AND e.workspace_id=request.workspace_id AND e.main_issue_id=p.issue_id)
 RETURNING *
), rebound AS (
 UPDATE workflow_review_request request SET task_id=child.id,status='queued',updated_at=now()
 FROM child WHERE request.id=child.workflow_review_request_id RETURNING request.id
)
SELECT child.* FROM child JOIN rebound ON rebound.id=child.workflow_review_request_id;

-- name: GetLatestWorkflowReviewRequest :one
SELECT * FROM workflow_review_request WHERE workspace_id=$1 AND execution_id=$2
AND status<>'invalidated' ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: CreateWorkflowReviewRecovery :one
INSERT INTO workflow_review_recovery(workspace_id,workflow_instance_id,execution_id,request_id,failed_task_id,new_task_id,result_revision,basis_hash,actor_id,reason,allowed_attempts,max_attempts)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetWorkflowReviewRecoveryByFailedCall :one
SELECT * FROM workflow_review_recovery WHERE workspace_id=$1 AND failed_task_id=$2;
