CREATE INDEX CONCURRENTLY idx_feedback_user_created ON public.feedback USING btree (user_id, created_at DESC);
