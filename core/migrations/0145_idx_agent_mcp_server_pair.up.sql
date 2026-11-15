CREATE UNIQUE INDEX CONCURRENTLY idx_agent_mcp_server_pair ON public.agent_mcp_server USING btree (agent_id, server_id);
