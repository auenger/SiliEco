CREATE UNIQUE INDEX CONCURRENTLY workflow_artifact_revision_number_idx ON public.workflow_artifact_revision USING btree (workspace_id, artifact_id, revision);
