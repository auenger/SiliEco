CREATE UNIQUE INDEX CONCURRENTLY workflow_instance_change_revision_idx ON public.workflow_instance_change USING btree (workflow_instance_id, revision);
