DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM workflow_rework WHERE source_execution_id IS NULL) THEN
        RAISE EXCEPTION 'cannot downgrade rework history while manual-origin records exist';
    END IF;
END $$;
UPDATE issue i SET workflow_invalidated_by_rework_id=r.source_execution_id FROM workflow_rework r WHERE i.workflow_invalidated_by_rework_id=r.id AND i.workspace_id=r.workspace_id;
UPDATE workflow_gate_decision d SET invalidated_by_rework_id=r.source_execution_id FROM workflow_rework r WHERE d.invalidated_by_rework_id=r.id AND d.workspace_id=r.workspace_id;
ALTER TABLE issue RENAME COLUMN workflow_invalidated_by_rework_id TO workflow_invalidated_by_execution_id;
ALTER TABLE workflow_gate_decision RENAME COLUMN invalidated_by_rework_id TO invalidated_by_execution_id;
DROP TABLE workflow_rework;
