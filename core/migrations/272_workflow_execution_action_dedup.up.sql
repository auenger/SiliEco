CREATE UNIQUE INDEX CONCURRENTLY workflow_execution_action_dedup_uidx ON workflow_execution_action (workspace_id, execution_id, kind);
