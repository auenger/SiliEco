CREATE UNIQUE INDEX CONCURRENTLY idx_pat_token_hash ON public.personal_access_token USING btree (token_hash);
