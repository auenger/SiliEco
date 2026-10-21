DROP TABLE workflow_artifact_revision;
DROP TABLE workflow_artifact_upload;
DROP TABLE workflow_artifact;
DROP TABLE workflow_execution_result;
ALTER TABLE workflow_stage_execution DROP COLUMN result_revision;
