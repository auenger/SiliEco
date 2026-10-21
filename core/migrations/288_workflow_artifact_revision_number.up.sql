CREATE UNIQUE INDEX CONCURRENTLY workflow_artifact_revision_number_idx ON workflow_artifact_revision (workspace_id, artifact_id, revision);
