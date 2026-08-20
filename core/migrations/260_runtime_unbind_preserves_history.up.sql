-- Runtime retirement must preserve agents, conversations, and task history.
-- NULL means the record is retained but no longer runnable until rebound.
ALTER TABLE agent
    ALTER COLUMN runtime_id DROP NOT NULL;

ALTER TABLE agent_task_queue
    ALTER COLUMN runtime_id DROP NOT NULL;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_active_requires_runtime
    CHECK (runtime_id IS NOT NULL OR completed_at IS NOT NULL)
    NOT VALID;
