CREATE UNIQUE INDEX CONCURRENTLY agent_system_identity_unique ON public.agent USING btree (workspace_id, owner_id, runtime_id, system_key) WHERE (system_key IS NOT NULL);
