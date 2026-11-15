CREATE INDEX CONCURRENTLY workflow_rework_instance_idx ON public.workflow_rework USING btree (workspace_id, workflow_instance_id);
