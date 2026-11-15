CREATE INDEX CONCURRENTLY comment_task_resolved_at_idx ON public.comment USING btree (task_id, resolved_at);
