CREATE INDEX CONCURRENTLY task_label_workspace_type_idx ON public.task_label USING btree (workspace_id, resource_type);
