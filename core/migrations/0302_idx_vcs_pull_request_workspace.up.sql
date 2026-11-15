CREATE INDEX CONCURRENTLY idx_vcs_pull_request_workspace ON public.vcs_pull_request USING btree (workspace_id);
