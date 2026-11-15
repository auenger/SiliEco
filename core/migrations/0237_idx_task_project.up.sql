CREATE INDEX CONCURRENTLY idx_task_project ON public.task USING btree (project_id);
