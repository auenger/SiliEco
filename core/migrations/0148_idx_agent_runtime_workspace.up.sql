CREATE INDEX CONCURRENTLY idx_agent_runtime_workspace ON public.agent_runtime USING btree (workspace_id);
