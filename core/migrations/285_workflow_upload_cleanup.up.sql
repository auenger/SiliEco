CREATE INDEX CONCURRENTLY workflow_upload_cleanup_idx ON workflow_artifact_upload (status, expires_at);
