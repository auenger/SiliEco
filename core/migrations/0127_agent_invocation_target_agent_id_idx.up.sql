CREATE INDEX CONCURRENTLY agent_invocation_target_agent_id_idx ON public.agent_invocation_target USING btree (agent_id);
