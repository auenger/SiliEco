CREATE INDEX CONCURRENTLY idx_task_property_workspace ON public.task_property USING btree (workspace_id);
