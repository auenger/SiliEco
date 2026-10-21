CREATE UNIQUE INDEX CONCURRENTLY workflow_result_revision_idx ON workflow_execution_result (workspace_id, execution_id, revision);
