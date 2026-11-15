CREATE INDEX CONCURRENTLY idx_channel_outbound_card_session ON public.channel_outbound_card_message USING btree (chat_session_id, created_at DESC);
