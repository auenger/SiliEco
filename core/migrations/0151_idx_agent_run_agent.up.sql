CREATE INDEX CONCURRENTLY idx_agent_run_agent ON public.agent_run USING btree (agent_id, status);
