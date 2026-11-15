CREATE INDEX CONCURRENTLY idx_project_title_trgm ON public.project USING gin (lower(title) public.gin_trgm_ops);
