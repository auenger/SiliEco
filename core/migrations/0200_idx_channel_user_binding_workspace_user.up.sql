CREATE INDEX CONCURRENTLY idx_channel_user_binding_workspace_user ON public.channel_user_binding USING btree (workspace_id, channel_user_id);
