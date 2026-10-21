ALTER TABLE workflow_stage ADD COLUMN retry_policy JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(retry_policy) = 'object');
ALTER TABLE workflow_instance_stage ADD COLUMN retry_policy JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(retry_policy) = 'object');
