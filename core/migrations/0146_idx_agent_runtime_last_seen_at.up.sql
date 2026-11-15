CREATE INDEX CONCURRENTLY idx_agent_runtime_last_seen_at ON public.agent_runtime USING btree (last_seen_at);
