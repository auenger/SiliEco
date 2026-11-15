CREATE INDEX CONCURRENTLY idx_autopilot_subscriber_user ON public.autopilot_subscriber USING btree (user_type, user_id);
