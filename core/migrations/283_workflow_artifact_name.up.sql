CREATE UNIQUE INDEX CONCURRENTLY workflow_artifact_name_idx ON workflow_artifact (workspace_id, workflow_instance_id, workflow_stage_id, name);
