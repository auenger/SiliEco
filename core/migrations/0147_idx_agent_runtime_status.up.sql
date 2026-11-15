CREATE INDEX CONCURRENTLY idx_agent_runtime_status ON public.agent_runtime USING btree (workspace_id, status);
