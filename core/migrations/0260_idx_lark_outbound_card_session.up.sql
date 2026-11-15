CREATE INDEX CONCURRENTLY idx_lark_outbound_card_session ON public.lark_outbound_card_message USING btree (chat_session_id, created_at DESC);
