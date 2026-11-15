CREATE INDEX CONCURRENTLY task_workflow_execution_idx ON public.task USING btree (workspace_id, workflow_stage_execution_id) WHERE (workflow_stage_execution_id IS NOT NULL);
