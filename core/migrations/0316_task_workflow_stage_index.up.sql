CREATE INDEX CONCURRENTLY task_workflow_stage_index ON public.task USING btree (workflow_instance_id, workflow_stage_id, "position") WHERE (workflow_instance_id IS NOT NULL);
