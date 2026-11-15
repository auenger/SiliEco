CREATE INDEX CONCURRENTLY workflow_workspace_status_index ON public.workflow USING btree (workspace_id, status, updated_at DESC);
