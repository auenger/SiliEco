CREATE UNIQUE INDEX CONCURRENTLY workflow_decision_basis_identity ON workflow_gate_decision(workspace_id,execution_id,basis_hash,review_step) WHERE execution_id IS NOT NULL;
