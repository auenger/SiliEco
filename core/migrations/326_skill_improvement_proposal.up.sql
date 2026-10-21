CREATE TABLE skill_improvement_proposal (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    skill_id UUID NOT NULL,
    baseline_revision_id UUID NOT NULL,
    candidate_revision_id UUID NOT NULL,
    request_id UUID NOT NULL,
    request_hash TEXT NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    category TEXT NOT NULL CHECK (category IN ('skill_instruction','insufficient_input','tool_failure','contract_issue','model_capability','human_correction','positive_example')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 8000),
    hypothesis TEXT NOT NULL CHECK (length(hypothesis) BETWEEN 1 AND 8000),
    applicable_scope TEXT NOT NULL CHECK (length(applicable_scope) BETWEEN 1 AND 4000),
    evidence JSONB NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (baseline_revision_id <> candidate_revision_id)
);

CREATE FUNCTION reject_skill_improvement_proposal_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'skill improvement proposals are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER skill_improvement_proposal_immutable
BEFORE UPDATE ON skill_improvement_proposal
FOR EACH ROW EXECUTE FUNCTION reject_skill_improvement_proposal_update();
