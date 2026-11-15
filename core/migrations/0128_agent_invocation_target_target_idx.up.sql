CREATE INDEX CONCURRENTLY agent_invocation_target_target_idx ON public.agent_invocation_target USING btree (target_type, target_id);
