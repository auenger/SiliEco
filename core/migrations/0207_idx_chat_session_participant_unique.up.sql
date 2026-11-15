CREATE UNIQUE INDEX CONCURRENTLY idx_chat_session_participant_unique ON public.chat_session_participant USING btree (chat_session_id, participant_type, participant_id);
