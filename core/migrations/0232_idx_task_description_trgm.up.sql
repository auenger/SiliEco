CREATE INDEX CONCURRENTLY idx_task_description_trgm ON public.task USING gin (lower(COALESCE(description, ''::text)) public.gin_trgm_ops);
