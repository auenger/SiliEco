CREATE UNIQUE INDEX CONCURRENTLY idx_daemon_token_hash ON public.daemon_token USING btree (token_hash);
