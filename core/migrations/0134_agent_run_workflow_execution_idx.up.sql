CREATE INDEX CONCURRENTLY agent_run_workflow_execution_idx ON public.agent_run USING btree (workflow_stage_execution_id) WHERE (workflow_stage_execution_id IS NOT NULL);
