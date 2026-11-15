CREATE INDEX CONCURRENTLY idx_comment_task_keyset ON public.comment USING btree (task_id, created_at DESC, id DESC);
