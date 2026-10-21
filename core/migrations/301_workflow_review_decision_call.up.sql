ALTER TABLE workflow_gate_decision ADD COLUMN review_task_id UUID;
UPDATE workflow_gate_decision decision SET review_task_id=request.task_id
FROM workflow_review_request request
WHERE decision.workspace_id=request.workspace_id AND decision.execution_id=request.execution_id
AND decision.basis_hash=request.basis_hash AND decision.review_step='agent';
