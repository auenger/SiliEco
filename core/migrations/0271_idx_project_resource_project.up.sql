CREATE INDEX CONCURRENTLY idx_project_resource_project ON public.project_resource USING btree (project_id, "position");
