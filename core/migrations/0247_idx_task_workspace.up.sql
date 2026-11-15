CREATE INDEX CONCURRENTLY idx_task_workspace ON public.task USING btree (workspace_id);
