ALTER TABLE workflow_review_request DROP COLUMN task_id;
ALTER TABLE agent_task_queue DROP CONSTRAINT workflow_call_role_exclusive, DROP COLUMN workflow_review_request_id;
