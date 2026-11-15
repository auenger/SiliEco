CREATE UNIQUE INDEX CONCURRENTLY workflow_rework_state_idx ON public.workflow_rework USING btree (workspace_id, workflow_instance_id, source_state_token);
