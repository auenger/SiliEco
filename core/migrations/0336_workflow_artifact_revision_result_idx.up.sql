CREATE INDEX CONCURRENTLY workflow_artifact_revision_result_idx ON public.workflow_artifact_revision USING btree (workspace_id, result_id);
