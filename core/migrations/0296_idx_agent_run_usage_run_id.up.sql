CREATE INDEX CONCURRENTLY idx_agent_run_usage_run_id ON public.agent_run_usage USING btree (run_id);
