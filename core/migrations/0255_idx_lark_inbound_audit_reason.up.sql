CREATE INDEX CONCURRENTLY idx_lark_inbound_audit_reason ON public.lark_inbound_audit USING btree (drop_reason, received_at DESC);
