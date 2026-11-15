CREATE INDEX CONCURRENTLY client_usage_daily_activity_client_user_idx ON public.client_usage_daily USING btree (activity_date, client_type, user_id);
