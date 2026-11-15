CREATE INDEX CONCURRENTLY idx_run_chat_finalize_deferred ON public.agent_run USING btree (chat_finalize_deferred_at) WHERE (chat_finalize_deferred_at IS NOT NULL);
