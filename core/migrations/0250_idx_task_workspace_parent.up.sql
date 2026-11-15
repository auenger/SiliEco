CREATE INDEX CONCURRENTLY idx_task_workspace_parent ON public.task USING btree (workspace_id, parent_task_id);
