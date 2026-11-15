CREATE INDEX CONCURRENTLY idx_vcs_connection_workspace ON public.vcs_connection USING btree (workspace_id);
