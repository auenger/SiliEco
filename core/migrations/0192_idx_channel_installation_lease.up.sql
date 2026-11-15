CREATE INDEX CONCURRENTLY idx_channel_installation_lease ON public.channel_installation USING btree (ws_lease_expires_at) WHERE (status = 'active'::text);
