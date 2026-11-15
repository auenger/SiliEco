CREATE INDEX CONCURRENTLY idx_channel_binding_token_installation ON public.channel_binding_token USING btree (installation_id, expires_at);
