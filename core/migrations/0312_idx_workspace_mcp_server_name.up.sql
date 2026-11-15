CREATE UNIQUE INDEX CONCURRENTLY idx_workspace_mcp_server_name ON public.workspace_mcp_server USING btree (workspace_id, name);
