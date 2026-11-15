CREATE INDEX CONCURRENTLY idx_autopilot_run_squad_id ON public.autopilot_run USING btree (squad_id) WHERE (squad_id IS NOT NULL);
