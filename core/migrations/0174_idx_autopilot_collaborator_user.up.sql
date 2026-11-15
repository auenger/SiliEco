CREATE INDEX CONCURRENTLY idx_autopilot_collaborator_user ON public.autopilot_collaborator USING btree (user_type, user_id);
