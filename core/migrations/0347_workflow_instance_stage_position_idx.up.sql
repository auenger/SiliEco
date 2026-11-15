CREATE UNIQUE INDEX CONCURRENTLY workflow_instance_stage_position_idx ON public.workflow_instance_stage USING btree (workflow_instance_id, "position");
