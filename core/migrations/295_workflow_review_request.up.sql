CREATE TABLE workflow_review_request (
 id UUID NOT NULL DEFAULT gen_random_uuid(),
 workspace_id UUID NOT NULL,
 workflow_instance_id UUID NOT NULL,
 execution_id UUID NOT NULL,
 reviewer_id UUID NOT NULL,
 result_id UUID NOT NULL,
 result_revision INTEGER NOT NULL CHECK(result_revision > 0),
 basis_hash TEXT NOT NULL CHECK(basis_hash ~ '^[0-9a-f]{64}$'),
 basis_snapshot JSONB NOT NULL CHECK(jsonb_typeof(basis_snapshot)='object'),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','queued','decided','invalidated')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
