ALTER TABLE workflow_stage_execution ADD COLUMN rework_source_execution_id UUID,
ADD COLUMN rework_metadata JSONB CHECK (rework_metadata IS NULL OR jsonb_typeof(rework_metadata)='object');
