CREATE INDEX CONCURRENTLY idx_attachment_task ON public.attachment USING btree (task_id) WHERE (task_id IS NOT NULL);
