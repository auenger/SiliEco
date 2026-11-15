CREATE INDEX CONCURRENTLY idx_workflow_instance_project_status ON public.workflow_instance USING btree (project_id, status, updated_at DESC);
