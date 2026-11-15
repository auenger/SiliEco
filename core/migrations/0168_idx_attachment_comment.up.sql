CREATE INDEX CONCURRENTLY idx_attachment_comment ON public.attachment USING btree (comment_id) WHERE (comment_id IS NOT NULL);
