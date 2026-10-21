ALTER TABLE workflow_instance DROP COLUMN inputs;
ALTER TABLE workflow_instance_stage DROP CONSTRAINT workflow_instance_stage_executor_check, DROP COLUMN executor_id, DROP COLUMN execution_mode;
ALTER TABLE workflow_stage DROP CONSTRAINT workflow_stage_executor_check, DROP COLUMN executor_id, DROP COLUMN execution_mode;
