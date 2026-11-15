CREATE INDEX CONCURRENTLY idx_chat_session_pinned ON public.chat_session USING btree (creator_id, workspace_id, pinned_at DESC) WHERE (pinned_at IS NOT NULL);
