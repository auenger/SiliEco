CREATE INDEX CONCURRENTLY idx_chat_session_participant_actor ON public.chat_session_participant USING btree (participant_type, participant_id, chat_session_id);
