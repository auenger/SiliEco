CREATE INDEX CONCURRENTLY idx_agent_run_usage_hourly_workspace_time ON public.agent_run_usage_hourly USING btree (workspace_id, bucket_hour DESC);
