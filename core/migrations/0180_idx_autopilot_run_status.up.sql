CREATE INDEX CONCURRENTLY idx_autopilot_run_status ON public.autopilot_run USING btree (autopilot_id, status) WHERE (status = ANY (ARRAY['task_created'::text, 'running'::text]));
