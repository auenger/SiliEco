CREATE INDEX CONCURRENTLY idx_task_first_executed_at ON public.task USING btree (workspace_id, first_executed_at) WHERE (first_executed_at IS NOT NULL);
