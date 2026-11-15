CREATE INDEX CONCURRENTLY idx_pat_user ON public.personal_access_token USING btree (user_id, revoked);
