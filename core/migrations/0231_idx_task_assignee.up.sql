CREATE INDEX CONCURRENTLY idx_task_assignee ON public.task USING btree (assignee_type, assignee_id);
