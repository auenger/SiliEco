CREATE INDEX CONCURRENTLY idx_agent_run_parent ON public.agent_run USING btree (parent_run_id);
