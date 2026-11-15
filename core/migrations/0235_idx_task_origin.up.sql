CREATE INDEX CONCURRENTLY idx_task_origin ON public.task USING btree (origin_type, origin_id) WHERE (origin_type IS NOT NULL);
