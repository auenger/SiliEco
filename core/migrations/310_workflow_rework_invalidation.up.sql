ALTER TABLE issue ADD COLUMN workflow_invalidated_by_execution_id UUID;
ALTER TABLE workflow_gate_decision ADD COLUMN invalidated_by_execution_id UUID;
