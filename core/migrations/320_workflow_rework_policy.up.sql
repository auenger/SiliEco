ALTER TABLE workflow_stage ADD COLUMN rework_policy JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(rework_policy) = 'object');
ALTER TABLE workflow_instance_stage ADD COLUMN rework_policy JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(rework_policy) = 'object');
