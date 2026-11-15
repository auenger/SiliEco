CREATE INDEX CONCURRENTLY idx_task_workspace_position ON public.task USING btree (workspace_id, "position", created_at DESC, id DESC);
