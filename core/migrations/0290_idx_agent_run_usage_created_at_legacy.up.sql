CREATE INDEX CONCURRENTLY idx_agent_run_usage_created_at_legacy ON public.agent_run_usage USING btree (created_at) WHERE (updated_at IS NULL);
