ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_active_requires_runtime;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id SET NOT NULL;

ALTER TABLE agent
    ALTER COLUMN runtime_id SET NOT NULL;
