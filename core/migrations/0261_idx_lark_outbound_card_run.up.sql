CREATE UNIQUE INDEX CONCURRENTLY idx_lark_outbound_card_run ON public.lark_outbound_card_message USING btree (run_id) WHERE (run_id IS NOT NULL);
