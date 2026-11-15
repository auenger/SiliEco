CREATE INDEX CONCURRENTLY idx_agent_run_claim_candidates ON public.agent_run USING btree (runtime_id, priority DESC, created_at) WHERE (status = 'queued'::text);
