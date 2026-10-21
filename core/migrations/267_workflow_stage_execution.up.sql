CREATE TABLE workflow_stage_execution (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    workflow_stage_id UUID NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    plan_revision INTEGER NOT NULL CHECK (plan_revision >= 1),
    executor_id UUID NOT NULL,
    main_issue_id UUID,
    config_snapshot JSONB NOT NULL CHECK (jsonb_typeof(config_snapshot) = 'object'),
    input_snapshot JSONB NOT NULL CHECK (jsonb_typeof(input_snapshot) = 'object'),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'queued', 'running', 'awaiting_review', 'succeeded', 'blocked', 'failed', 'cancelled')),
    failure_code TEXT,
    failure_detail TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE TABLE workflow_execution_action (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    workflow_instance_id UUID NOT NULL,
    execution_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('dispatch', 'review', 'advance')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'leased', 'delivered', 'cancelled')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    claim_token UUID,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status = 'leased' AND lease_until IS NOT NULL AND claim_token IS NOT NULL)
        OR (status <> 'leased' AND lease_until IS NULL AND claim_token IS NULL))
);
