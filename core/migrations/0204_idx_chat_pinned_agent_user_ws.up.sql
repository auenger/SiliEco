CREATE INDEX CONCURRENTLY idx_chat_pinned_agent_user_ws ON public.chat_pinned_agent USING btree (workspace_id, user_id, "position");
