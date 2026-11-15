CREATE INDEX CONCURRENTLY idx_agent_run_message_run_id_seq ON public.agent_run_message USING btree (run_id, seq);
