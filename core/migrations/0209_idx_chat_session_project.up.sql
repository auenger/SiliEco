CREATE INDEX CONCURRENTLY idx_chat_session_project ON public.chat_session USING btree (project_id) WHERE (project_id IS NOT NULL);
