CREATE UNIQUE INDEX CONCURRENTLY workflow_stage_key_index ON public.workflow_stage USING btree (workflow_version_id, stable_key);
