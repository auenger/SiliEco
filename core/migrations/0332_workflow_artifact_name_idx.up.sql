CREATE UNIQUE INDEX CONCURRENTLY workflow_artifact_name_idx ON public.workflow_artifact USING btree (workspace_id, workflow_instance_id, workflow_stage_id, name);
