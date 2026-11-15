CREATE INDEX CONCURRENTLY idx_task_properties_gin ON public.task USING gin (properties jsonb_path_ops);
