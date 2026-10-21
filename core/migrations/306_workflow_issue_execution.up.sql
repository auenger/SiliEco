ALTER TABLE issue ADD COLUMN workflow_stage_execution_id UUID;
UPDATE issue i SET workflow_stage_execution_id=(
 SELECT e.id FROM workflow_stage_execution e
 WHERE e.workspace_id=i.workspace_id AND e.workflow_instance_id=i.workflow_instance_id
 AND e.workflow_stage_id=i.workflow_stage_id ORDER BY e.attempt DESC LIMIT 1
) WHERE i.workflow_instance_id IS NOT NULL AND i.workflow_stage_id IS NOT NULL;
