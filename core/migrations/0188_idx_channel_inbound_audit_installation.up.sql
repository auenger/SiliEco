CREATE INDEX CONCURRENTLY idx_channel_inbound_audit_installation ON public.channel_inbound_audit USING btree (installation_id, received_at DESC);
