CREATE INDEX CONCURRENTLY idx_agent_run_task_id ON public.agent_run USING btree (task_id);
