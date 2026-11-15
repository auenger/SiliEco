CREATE INDEX CONCURRENTLY idx_autopilot_assignee_type_id ON public.autopilot USING btree (assignee_type, assignee_id);
