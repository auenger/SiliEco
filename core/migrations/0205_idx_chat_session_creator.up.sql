CREATE INDEX CONCURRENTLY idx_chat_session_creator ON public.chat_session USING btree (creator_id, workspace_id);
