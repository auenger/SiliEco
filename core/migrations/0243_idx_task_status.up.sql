CREATE INDEX CONCURRENTLY idx_task_status ON public.task USING btree (workspace_id, status);
