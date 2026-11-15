CREATE INDEX CONCURRENTLY idx_task_workspace_assignee ON public.task USING btree (workspace_id, assignee_type, assignee_id);
