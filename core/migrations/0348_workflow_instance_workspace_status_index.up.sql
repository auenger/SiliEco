CREATE INDEX CONCURRENTLY workflow_instance_workspace_status_index ON public.workflow_instance USING btree (workspace_id, status, updated_at DESC);
