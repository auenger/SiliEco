ALTER TABLE workflow_instance_stage
    ADD COLUMN required_skill_snapshots JSONB NOT NULL DEFAULT '[]'::jsonb;
