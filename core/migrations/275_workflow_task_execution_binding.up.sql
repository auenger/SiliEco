ALTER TABLE agent_task_queue ADD COLUMN workflow_stage_execution_id UUID;
ALTER TABLE workflow_stage_execution ADD COLUMN main_task_id UUID;
