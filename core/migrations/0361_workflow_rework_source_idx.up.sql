CREATE UNIQUE INDEX CONCURRENTLY workflow_rework_source_idx ON public.workflow_rework USING btree (workspace_id, source_execution_id);
