CREATE UNIQUE INDEX CONCURRENTLY workflow_instance_stage_key_idx ON public.workflow_instance_stage USING btree (workflow_instance_id, stable_key);
