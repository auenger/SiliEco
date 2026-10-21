ALTER TABLE workflow_stage
    ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'manual' CHECK (execution_mode IN ('manual', 'agent')),
    ADD COLUMN executor_id UUID,
    ADD CONSTRAINT workflow_stage_executor_check CHECK (
        (execution_mode = 'agent' AND executor_id IS NOT NULL) OR (execution_mode = 'manual' AND executor_id IS NULL)
    );
ALTER TABLE workflow_instance_stage
    ADD COLUMN execution_mode TEXT NOT NULL DEFAULT 'manual' CHECK (execution_mode IN ('manual', 'agent')),
    ADD COLUMN executor_id UUID,
    ADD CONSTRAINT workflow_instance_stage_executor_check CHECK (
        (execution_mode = 'agent' AND executor_id IS NOT NULL) OR (execution_mode = 'manual' AND executor_id IS NULL)
    );
ALTER TABLE workflow_instance
    ADD COLUMN inputs JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(inputs) = 'object');
