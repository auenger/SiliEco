CREATE INDEX CONCURRENTLY idx_channel_installation_workspace ON public.channel_installation USING btree (workspace_id);
