CREATE TABLE workspace_mcp_server (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    config_encrypted TEXT NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_mcp_server (
    agent_id UUID NOT NULL,
    server_id UUID NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE workflow_stage
    ADD COLUMN allowed_mcp_server_ids UUID[] NULL;

ALTER TABLE workflow_instance_stage
    ADD COLUMN allowed_mcp_server_ids UUID[] NULL;

ALTER TABLE agent_task_queue
    ADD COLUMN execution_manifest JSONB NULL;
