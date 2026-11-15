CREATE UNIQUE INDEX CONCURRENTLY agent_runtime_workspace_daemon_profile_key ON public.agent_runtime USING btree (workspace_id, daemon_id, profile_id) WHERE (profile_id IS NOT NULL);
