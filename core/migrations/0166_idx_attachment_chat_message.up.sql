CREATE INDEX CONCURRENTLY idx_attachment_chat_message ON public.attachment USING btree (chat_message_id) WHERE (chat_message_id IS NOT NULL);
