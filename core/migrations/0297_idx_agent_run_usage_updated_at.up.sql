CREATE INDEX CONCURRENTLY idx_agent_run_usage_updated_at ON public.agent_run_usage USING btree (updated_at);
