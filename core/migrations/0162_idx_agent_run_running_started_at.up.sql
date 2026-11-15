CREATE INDEX CONCURRENTLY idx_agent_run_running_started_at ON public.agent_run USING btree (started_at) WHERE (status = 'running'::text);
