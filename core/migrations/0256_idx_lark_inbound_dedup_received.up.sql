CREATE INDEX CONCURRENTLY idx_lark_inbound_dedup_received ON public.lark_inbound_message_dedup USING btree (received_at);
