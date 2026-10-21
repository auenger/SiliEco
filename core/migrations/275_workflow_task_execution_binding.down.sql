ALTER TABLE workflow_stage_execution DROP COLUMN main_task_id;
ALTER TABLE agent_task_queue DROP COLUMN workflow_stage_execution_id;
