CREATE UNIQUE INDEX CONCURRENTLY workflow_stage_position_index ON public.workflow_stage USING btree (workflow_version_id, "position");
