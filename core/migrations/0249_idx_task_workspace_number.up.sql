CREATE INDEX CONCURRENTLY idx_task_workspace_number ON public.task USING btree (workspace_id, number);
