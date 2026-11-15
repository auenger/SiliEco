CREATE INDEX CONCURRENTLY idx_github_installation_workspace ON public.github_installation USING btree (workspace_id);
