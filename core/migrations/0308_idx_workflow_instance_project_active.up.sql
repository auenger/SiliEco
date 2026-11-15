CREATE INDEX CONCURRENTLY idx_workflow_instance_project_active ON public.workflow_instance USING btree (workspace_id, project_id, updated_at DESC) WHERE (archived_at IS NULL);
