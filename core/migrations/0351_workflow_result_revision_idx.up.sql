CREATE UNIQUE INDEX CONCURRENTLY workflow_result_revision_idx ON public.workflow_execution_result USING btree (workspace_id, execution_id, revision);
