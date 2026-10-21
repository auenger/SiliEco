CREATE UNIQUE INDEX CONCURRENTLY workflow_rework_state_idx ON workflow_rework (workspace_id, workflow_instance_id, source_state_token);
