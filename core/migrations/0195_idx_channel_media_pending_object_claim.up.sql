CREATE INDEX CONCURRENTLY idx_channel_media_pending_object_claim ON public.channel_media_pending_object USING btree (state, next_attempt_at);
