CREATE INDEX CONCURRENTLY idx_agent_run_usage_hourly_runtime_time ON public.agent_run_usage_hourly USING btree (runtime_id, bucket_hour DESC);
