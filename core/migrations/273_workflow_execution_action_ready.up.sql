CREATE INDEX CONCURRENTLY workflow_execution_action_ready_idx ON workflow_execution_action (workspace_id, available_at, lease_until) WHERE status IN ('pending', 'leased');
