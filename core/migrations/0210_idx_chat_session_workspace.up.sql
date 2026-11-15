CREATE INDEX CONCURRENTLY idx_chat_session_workspace ON public.chat_session USING btree (workspace_id);
