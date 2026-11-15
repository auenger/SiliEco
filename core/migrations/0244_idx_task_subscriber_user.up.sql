CREATE INDEX CONCURRENTLY idx_task_subscriber_user ON public.task_subscriber USING btree (user_type, user_id);
