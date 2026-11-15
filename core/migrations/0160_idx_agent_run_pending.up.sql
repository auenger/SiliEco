CREATE INDEX CONCURRENTLY idx_agent_run_pending ON public.agent_run USING btree (agent_id, priority DESC, created_at) WHERE (status = ANY (ARRAY['queued'::text, 'dispatched'::text]));
