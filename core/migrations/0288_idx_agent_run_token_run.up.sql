CREATE INDEX CONCURRENTLY idx_agent_run_token_run ON public.agent_run_token USING btree (run_id);
