CREATE UNIQUE INDEX CONCURRENTLY idx_agent_run_token_hash ON public.agent_run_token USING btree (token_hash);
