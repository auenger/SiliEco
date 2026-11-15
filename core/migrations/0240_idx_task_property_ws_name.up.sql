CREATE UNIQUE INDEX CONCURRENTLY idx_task_property_ws_name ON public.task_property USING btree (workspace_id, lower(name));
