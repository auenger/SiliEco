CREATE INDEX CONCURRENTLY idx_project_resource_workspace ON public.project_resource USING btree (workspace_id);
