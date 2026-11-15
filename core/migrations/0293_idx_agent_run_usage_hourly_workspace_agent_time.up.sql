CREATE INDEX CONCURRENTLY idx_agent_run_usage_hourly_workspace_agent_time ON public.agent_run_usage_hourly USING btree (workspace_id, agent_id, bucket_hour DESC);
