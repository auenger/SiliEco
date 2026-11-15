CREATE INDEX CONCURRENTLY idx_channel_user_binding_user ON public.channel_user_binding USING btree (silieco_user_id, workspace_id);
