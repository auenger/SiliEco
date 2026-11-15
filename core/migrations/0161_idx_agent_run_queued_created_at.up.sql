CREATE INDEX CONCURRENTLY idx_agent_run_queued_created_at ON public.agent_run USING btree (created_at) WHERE (status = 'queued'::text);
