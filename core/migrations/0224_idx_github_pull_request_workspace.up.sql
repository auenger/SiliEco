CREATE INDEX CONCURRENTLY idx_github_pull_request_workspace ON public.github_pull_request USING btree (workspace_id);
