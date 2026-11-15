CREATE INDEX CONCURRENTLY idx_attachment_chat_session ON public.attachment USING btree (chat_session_id) WHERE (chat_session_id IS NOT NULL);
