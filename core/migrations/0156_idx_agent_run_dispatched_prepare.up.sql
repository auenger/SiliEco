CREATE INDEX CONCURRENTLY idx_agent_run_dispatched_prepare ON public.agent_run USING btree (runtime_id, priority DESC, dispatched_at) WHERE ((status = 'dispatched'::text) AND (started_at IS NULL));
