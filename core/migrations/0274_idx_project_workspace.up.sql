CREATE INDEX CONCURRENTLY idx_project_workspace ON public.project USING btree (workspace_id);
