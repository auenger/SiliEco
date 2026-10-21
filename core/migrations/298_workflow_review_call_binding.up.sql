ALTER TABLE agent_task_queue
 ADD COLUMN workflow_review_request_id UUID,
 ADD CONSTRAINT workflow_call_role_exclusive CHECK (workflow_review_request_id IS NULL OR workflow_stage_execution_id IS NULL);
ALTER TABLE workflow_review_request ADD COLUMN task_id UUID;
