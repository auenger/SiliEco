CREATE UNIQUE INDEX CONCURRENTLY agent_runtime_workspace_daemon_provider_key ON public.agent_runtime USING btree (workspace_id, daemon_id, provider) WHERE (profile_id IS NULL);
