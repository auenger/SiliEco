CREATE UNIQUE INDEX CONCURRENTLY lark_inbound_message_dedup_pkey ON public.lark_inbound_message_dedup (installation_id, message_id);
