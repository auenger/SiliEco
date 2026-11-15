CREATE INDEX CONCURRENTLY workflow_result_instance_idx ON public.workflow_execution_result USING btree (workspace_id, workflow_instance_id);
