CREATE UNIQUE INDEX CONCURRENTLY workflow_execution_action_dedup_uidx ON public.workflow_execution_action USING btree (workspace_id, execution_id, kind);
