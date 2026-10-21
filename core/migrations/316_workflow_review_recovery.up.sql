CREATE TABLE workflow_review_recovery (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    execution_id UUID NOT NULL,
    request_id UUID NOT NULL,
    failed_task_id UUID NOT NULL,
    new_task_id UUID NOT NULL,
    result_revision INTEGER NOT NULL CHECK (result_revision > 0),
    basis_hash TEXT NOT NULL CHECK (length(basis_hash)=64),
    actor_id UUID NOT NULL,
    reason TEXT NOT NULL CHECK (length(btrim(reason)) > 0 AND octet_length(reason) <= 4000),
    allowed_attempts INTEGER NOT NULL CHECK (allowed_attempts BETWEEN 1 AND 3),
    max_attempts INTEGER NOT NULL CHECK (max_attempts > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
