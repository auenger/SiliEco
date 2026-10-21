CREATE UNIQUE INDEX CONCURRENTLY workflow_stage_execution_attempt_uidx ON workflow_stage_execution (workspace_id, workflow_instance_id, workflow_stage_id, attempt);
