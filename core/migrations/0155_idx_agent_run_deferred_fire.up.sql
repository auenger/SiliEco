CREATE INDEX CONCURRENTLY idx_agent_run_deferred_fire ON public.agent_run USING btree (runtime_id, fire_at) WHERE (status = 'deferred'::text);
