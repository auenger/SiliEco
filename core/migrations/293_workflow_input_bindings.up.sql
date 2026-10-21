ALTER TABLE workflow_stage ADD COLUMN input_bindings JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(input_bindings)='object');
ALTER TABLE workflow_instance_stage ADD COLUMN input_bindings JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(input_bindings)='object');
