-- name: LockWorkflowReviewTasks :many
SELECT * FROM task WHERE task.workspace_id=$1 AND workflow_instance_id=$2 AND workflow_stage_id=$3
AND workflow_invalidated_by_rework_id IS NULL
AND (workflow_stage_execution_id IS NULL OR workflow_stage_execution_id=$4)
ORDER BY id FOR UPDATE;

-- name: LockWorkflowReviewCalls :many
-- Lock all calls that affect the completion barrier, including historical bound
-- calls whose task was moved. Insertions serialize on the parent Run lock.
SELECT run.id, run.agent_id, run.task_id, run.status, run.attempt,
       run.workflow_stage_execution_id, run.started_at, run.completed_at, run.created_at
FROM agent_run run
WHERE run.workflow_review_request_id IS NULL AND ((run.workflow_stage_execution_id=sqlc.arg(execution_id) AND EXISTS (
    SELECT 1 FROM workflow_stage_execution execution WHERE execution.id=run.workflow_stage_execution_id
    AND execution.workspace_id=sqlc.arg(workspace_id) AND execution.workflow_instance_id=sqlc.arg(instance_id)))
OR (run.workflow_stage_execution_id IS NULL AND EXISTS (
    SELECT 1 FROM task WHERE task.id=run.task_id AND task.workspace_id=sqlc.arg(workspace_id)
    AND task.workflow_instance_id=sqlc.arg(instance_id) AND task.workflow_stage_id=sqlc.arg(stage_id)
    AND task.workflow_invalidated_by_rework_id IS NULL
    AND (task.workflow_stage_execution_id IS NULL OR task.workflow_stage_execution_id=sqlc.arg(execution_id)))))
ORDER BY run.id FOR UPDATE OF run;

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
SELECT * FROM workflow_review_request WHERE workflow_review_request.workspace_id=$1 AND execution_id=$2 AND basis_hash=$3;

-- name: InvalidateWorkflowReviewRequests :exec
UPDATE workflow_review_request SET status='invalidated',updated_at=now()
WHERE workspace_id=$1 AND execution_id=$2 AND basis_hash<>$3 AND status IN ('pending','queued');

-- name: BindWorkflowReviewCall :one
-- Caller holds the parent Run lock and revalidates current request evidence.
WITH bound_call AS (
 UPDATE agent_run run SET workflow_review_request_id=request.id, max_attempts=COALESCE(sqlc.narg(max_attempts)::integer,run.max_attempts)
 FROM workflow_review_request request
 JOIN workflow_stage_execution execution ON execution.id=request.execution_id AND execution.workspace_id=request.workspace_id
 WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
 AND request.status='pending' AND request.run_id IS NULL
 AND run.id=sqlc.arg(run_id) AND run.status='queued' AND run.agent_id=request.reviewer_id
 AND run.task_id=execution.main_task_id
 AND run.workflow_stage_execution_id IS NULL AND run.workflow_review_request_id IS NULL
 RETURNING run.id
)
UPDATE workflow_review_request request SET run_id=bound_call.id,status='queued',updated_at=now()
FROM bound_call WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
RETURNING request.*;

-- name: GetWorkflowReviewRequest :one
SELECT * FROM workflow_review_request WHERE workflow_review_request.workspace_id=$1 AND id=$2;

-- name: CreateWorkflowAgentDecision :one
INSERT INTO workflow_gate_decision(workspace_id,workflow_instance_id,from_stage_id,outcome,actor_type,actor_id,note,execution_id,result_revision,review_step,basis_hash,basis_snapshot,review_run_id)
VALUES($1,$2,$3,$4,'agent',$5,$6,$7,$8,'agent',$9,$10,$11) RETURNING *;

-- name: GetWorkflowAgentDecisionByBasis :one
SELECT * FROM workflow_gate_decision WHERE workflow_gate_decision.workspace_id=$1 AND execution_id=$2 AND basis_hash=$3 AND review_step='agent' AND review_run_id=$4;

-- name: MarkWorkflowReviewDecided :one
UPDATE workflow_review_request SET status='decided',updated_at=now()
WHERE workspace_id=$1 AND id=$2 AND status='queued' AND run_id=$3 RETURNING *;

-- name: ListQueuedWorkflowReviewsForAgent :many
SELECT request.* FROM workflow_review_request request
JOIN agent_run run ON run.id=request.run_id AND run.workflow_review_request_id=request.id
WHERE request.reviewer_id=$1 AND request.status='queued' AND run.status='queued'
ORDER BY run.priority DESC,run.created_at,request.id LIMIT 100;

-- name: ClaimWorkflowReviewCall :one
UPDATE agent_run run SET status='dispatched',dispatched_at=now(),
 prepare_lease_expires_at=now()+make_interval(secs=>sqlc.arg(prepare_lease_secs)::double precision)
WHERE run.id=sqlc.arg(run_id) AND run.workflow_review_request_id=sqlc.arg(request_id)
 AND run.status='queued'
 AND NOT EXISTS(SELECT 1 FROM agent_run active WHERE active.agent_id=run.agent_id
 AND active.task_id=run.task_id AND active.status IN ('dispatched','running','waiting_local_directory'))
RETURNING run.*;

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
 INSERT INTO agent_run (
  agent_id,runtime_id,task_id,status,priority,trigger_comment_id,coalesced_comment_ids,
  trigger_summary,context,work_dir,attempt,max_attempts,parent_run_id,retry_of_run_id,
  force_fresh_session,handoff_note,is_leader_run,squad_id,originator_user_id,accountable_user_id,
  runtime_mcp_overlay,runtime_connected_apps,originator_source,delegated_from_run_id,rule_version_id,
  trigger_evidence_kind,trigger_evidence_ref_id,workflow_review_request_id
 )
 SELECT p.agent_id,sqlc.arg(runtime_id)::uuid,p.task_id,'queued',p.priority,p.trigger_comment_id,p.coalesced_comment_ids,
  p.trigger_summary,p.context,CASE WHEN p.runtime_id=sqlc.arg(runtime_id)::uuid THEN p.work_dir ELSE NULL END,p.attempt+1,sqlc.arg(max_attempts),p.id,p.id,
  true,p.handoff_note,p.is_leader_run,p.squad_id,p.originator_user_id,p.accountable_user_id,
  sqlc.arg(runtime_mcp_overlay),sqlc.arg(runtime_connected_apps),p.originator_source,p.delegated_from_run_id,p.rule_version_id,
  p.trigger_evidence_kind,p.trigger_evidence_ref_id,request.id
 FROM workflow_review_request request JOIN agent_run p ON p.id=request.run_id AND p.workflow_review_request_id=request.id
 WHERE request.workspace_id=sqlc.arg(workspace_id) AND request.id=sqlc.arg(request_id)
 AND request.run_id=sqlc.arg(parent_run_id) AND request.status IN ('queued','decided')
 AND (p.status='failed' OR (p.status='completed' AND NOT EXISTS (SELECT 1 FROM workflow_gate_decision d WHERE d.workspace_id=request.workspace_id AND d.execution_id=request.execution_id AND d.basis_hash=request.basis_hash AND d.review_run_id=p.id AND d.review_step='agent'))) AND p.attempt<sqlc.arg(max_attempts) AND p.workflow_stage_execution_id IS NULL
 AND p.agent_id=request.reviewer_id AND EXISTS (SELECT 1 FROM workflow_stage_execution e WHERE e.id=request.execution_id AND e.workspace_id=request.workspace_id AND e.main_task_id=p.task_id)
 AND (p.agent_id IS NULL OR EXISTS (SELECT 1 FROM agent ref_agent_id WHERE ref_agent_id.id = p.agent_id FOR KEY SHARE))
 AND (p.task_id IS NULL OR EXISTS (SELECT 1 FROM task ref_task_id WHERE ref_task_id.id = p.task_id FOR KEY SHARE))
 AND (p.trigger_comment_id IS NULL OR EXISTS (SELECT 1 FROM comment ref_trigger_comment_id WHERE ref_trigger_comment_id.id = p.trigger_comment_id FOR KEY SHARE))
 AND EXISTS (SELECT 1 FROM agent_runtime ref_runtime WHERE ref_runtime.id = sqlc.arg(runtime_id) AND ref_runtime.workspace_id = request.workspace_id FOR KEY SHARE)
 FOR KEY SHARE OF p
 RETURNING *
), rebound AS (
 UPDATE workflow_review_request request SET run_id=child.id,status='queued',updated_at=now()
 FROM child WHERE request.id=child.workflow_review_request_id RETURNING request.id
)
SELECT child.* FROM child JOIN rebound ON rebound.id=child.workflow_review_request_id;

-- name: GetLatestWorkflowReviewRequest :one
SELECT * FROM workflow_review_request WHERE workflow_review_request.workspace_id=$1 AND execution_id=$2
AND status<>'invalidated' ORDER BY created_at DESC,id DESC LIMIT 1;

-- name: CreateWorkflowReviewRecovery :one
INSERT INTO workflow_review_recovery(workspace_id,workflow_instance_id,execution_id,request_id,failed_run_id,new_run_id,result_revision,basis_hash,actor_id,reason,allowed_attempts,max_attempts)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetWorkflowReviewRecoveryByFailedCall :one
SELECT * FROM workflow_review_recovery WHERE workflow_review_recovery.workspace_id=$1 AND failed_run_id=$2;
