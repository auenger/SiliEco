ALTER TABLE workflow_gate_decision
    DROP CONSTRAINT workflow_decision_basis_check,
    DROP COLUMN execution_id,
    DROP COLUMN result_revision,
    DROP COLUMN review_step,
    DROP COLUMN basis_hash,
    DROP COLUMN basis_snapshot;
