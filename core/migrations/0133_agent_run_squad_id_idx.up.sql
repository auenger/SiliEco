CREATE INDEX CONCURRENTLY agent_run_squad_id_idx ON public.agent_run USING btree (squad_id) WHERE (squad_id IS NOT NULL);
