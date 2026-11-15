CREATE UNIQUE INDEX CONCURRENTLY workflow_result_id_idx ON public.workflow_execution_result USING btree (id);
