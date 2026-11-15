CREATE INDEX CONCURRENTLY idx_autopilot_run_task ON public.autopilot_run USING btree (task_id) WHERE (task_id IS NOT NULL);
