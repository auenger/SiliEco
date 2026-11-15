CREATE INDEX CONCURRENTLY idx_lark_inbound_audit_installation ON public.lark_inbound_audit USING btree (installation_id, received_at DESC);
