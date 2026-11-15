CREATE INDEX CONCURRENTLY idx_task_metadata_gin ON public.task USING gin (metadata jsonb_path_ops);
