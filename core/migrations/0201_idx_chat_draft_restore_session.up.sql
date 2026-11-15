CREATE INDEX CONCURRENTLY idx_chat_draft_restore_session ON public.chat_draft_restore USING btree (chat_session_id);
