CREATE INDEX CONCURRENTLY idx_autopilot_trigger_next_run ON public.autopilot_trigger USING btree (next_run_at) WHERE ((enabled = true) AND (kind = 'schedule'::text));
