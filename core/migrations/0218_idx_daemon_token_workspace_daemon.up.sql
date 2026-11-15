CREATE INDEX CONCURRENTLY idx_daemon_token_workspace_daemon ON public.daemon_token USING btree (workspace_id, daemon_id);
