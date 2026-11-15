CREATE INDEX CONCURRENTLY workflow_instance_definition_index ON public.workflow_instance USING btree (workflow_id, workflow_version_id, created_at DESC);
