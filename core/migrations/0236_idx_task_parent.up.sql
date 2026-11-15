CREATE INDEX CONCURRENTLY idx_task_parent ON public.task USING btree (parent_task_id);
