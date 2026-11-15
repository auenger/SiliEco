CREATE INDEX CONCURRENTLY client_usage_daily_workspace_idx ON public.client_usage_daily USING btree (workspace_id) WHERE (workspace_id IS NOT NULL);
