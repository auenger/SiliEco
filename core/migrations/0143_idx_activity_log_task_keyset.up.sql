CREATE INDEX CONCURRENTLY idx_activity_log_task_keyset ON public.activity_log USING btree (task_id, created_at DESC, id DESC);
