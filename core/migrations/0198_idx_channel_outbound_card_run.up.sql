CREATE UNIQUE INDEX CONCURRENTLY idx_channel_outbound_card_run ON public.channel_outbound_card_message USING btree (run_id) WHERE (run_id IS NOT NULL);
