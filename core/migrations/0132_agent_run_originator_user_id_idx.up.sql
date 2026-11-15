CREATE INDEX CONCURRENTLY agent_run_originator_user_id_idx ON public.agent_run USING btree (originator_user_id) WHERE (originator_user_id IS NOT NULL);
