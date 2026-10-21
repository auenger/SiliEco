ALTER TABLE workflow_gate_decision
    ADD COLUMN execution_id UUID,
    ADD COLUMN result_revision INTEGER,
    ADD COLUMN review_step TEXT,
    ADD COLUMN basis_hash TEXT,
    ADD COLUMN basis_snapshot JSONB,
    ADD CONSTRAINT workflow_decision_basis_check CHECK (
        (execution_id IS NULL AND result_revision IS NULL AND review_step IS NULL AND basis_hash IS NULL AND basis_snapshot IS NULL)
        OR (execution_id IS NOT NULL AND result_revision > 0 AND result_revision IS NOT NULL
            AND review_step IN ('human','agent','automatic') AND review_step IS NOT NULL
            AND basis_hash ~ '^[0-9a-f]{64}$' AND basis_hash IS NOT NULL
            AND jsonb_typeof(basis_snapshot)='object' AND basis_snapshot IS NOT NULL)
    );
