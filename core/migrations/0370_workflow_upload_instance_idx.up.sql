CREATE INDEX CONCURRENTLY workflow_upload_instance_idx ON public.workflow_artifact_upload USING btree (workspace_id, workflow_instance_id);
