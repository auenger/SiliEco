CREATE INDEX CONCURRENTLY idx_project_description_trgm ON public.project USING gin (lower(COALESCE(description, ''::text)) public.gin_trgm_ops);
