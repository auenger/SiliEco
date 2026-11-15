CREATE INDEX CONCURRENTLY idx_agent_run_usage_created_at ON public.agent_run_usage USING btree (created_at);
