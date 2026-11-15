CREATE INDEX CONCURRENTLY idx_workflow_project_status ON public.workflow USING btree (project_id, status, updated_at DESC);
