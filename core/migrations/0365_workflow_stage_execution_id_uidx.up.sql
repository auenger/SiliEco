CREATE UNIQUE INDEX CONCURRENTLY workflow_stage_execution_id_uidx ON public.workflow_stage_execution USING btree (id);
