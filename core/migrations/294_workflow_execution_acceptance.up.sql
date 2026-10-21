ALTER TABLE workflow_stage_execution ADD COLUMN acceptance JSONB CHECK (acceptance IS NULL OR jsonb_typeof(acceptance)='object');
