CREATE INDEX CONCURRENTLY idx_autopilot_run_autopilot ON public.autopilot_run USING btree (autopilot_id, created_at DESC);
