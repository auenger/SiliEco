CREATE INDEX CONCURRENTLY idx_comment_reaction_comment_id ON public.comment_reaction USING btree (comment_id);
