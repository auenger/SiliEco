CREATE TABLE workflow_rework (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    source_stage_id UUID NOT NULL,
    target_stage_id UUID NOT NULL,
    source_execution_id UUID,
    source_state_token TEXT,
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member','agent')),
    actor_id UUID NOT NULL,
    reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (source_execution_id IS NOT NULL OR (source_state_token IS NOT NULL AND length(source_state_token)=64))
);

INSERT INTO workflow_rework(id,workspace_id,workflow_instance_id,source_stage_id,target_stage_id,source_execution_id,plan_revision,actor_type,actor_id,reason,created_at)
SELECT source.id,source.workspace_id,source.workflow_instance_id,source.workflow_stage_id,replacement.workflow_stage_id,source.id,replacement.plan_revision,'member',
       COALESCE((replacement.rework_metadata->>'actor_id')::uuid,run.created_by),
       COALESCE(replacement.rework_metadata->>'reason',''),replacement.created_at
FROM workflow_stage_execution replacement
JOIN workflow_stage_execution source ON source.id=replacement.rework_source_execution_id AND source.workspace_id=replacement.workspace_id
JOIN workflow_instance run ON run.id=source.workflow_instance_id AND run.workspace_id=source.workspace_id;

ALTER TABLE issue RENAME COLUMN workflow_invalidated_by_execution_id TO workflow_invalidated_by_rework_id;
ALTER TABLE workflow_gate_decision RENAME COLUMN invalidated_by_execution_id TO invalidated_by_rework_id;
