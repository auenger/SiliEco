CREATE INDEX CONCURRENTLY idx_task_title_trgm ON public.task USING gin (lower(title) public.gin_trgm_ops);
