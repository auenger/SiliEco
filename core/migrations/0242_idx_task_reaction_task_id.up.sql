CREATE INDEX CONCURRENTLY idx_task_reaction_task_id ON public.task_reaction USING btree (task_id);
