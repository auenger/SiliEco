CREATE INDEX CONCURRENTLY idx_channel_inbound_audit_reason ON public.channel_inbound_audit USING btree (drop_reason, received_at DESC);
