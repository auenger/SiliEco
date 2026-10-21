ALTER TABLE workflow_stage_execution ADD COLUMN result_revision INTEGER NOT NULL DEFAULT 0 CHECK (result_revision >= 0);
CREATE TABLE workflow_execution_result (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    execution_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    data JSONB NOT NULL CHECK (jsonb_typeof(data) = 'object'),
    submitted_by_type TEXT NOT NULL CHECK (submitted_by_type IN ('member','agent')),
    submitted_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE workflow_artifact (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    workflow_stage_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0)
);
CREATE TABLE workflow_artifact_upload (
    id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    storage_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','committed','deleting')),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '1 hour',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE workflow_artifact_revision (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    artifact_id UUID NOT NULL,
    execution_id UUID NOT NULL,
    result_id UUID NOT NULL,
    upload_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    source_attachment_id UUID NOT NULL,
    kind TEXT NOT NULL,
    media_type TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    checksum TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
