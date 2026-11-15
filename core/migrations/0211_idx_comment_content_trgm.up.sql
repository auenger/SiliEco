CREATE INDEX CONCURRENTLY idx_comment_content_trgm ON public.comment USING gin (lower(content) public.gin_trgm_ops);
