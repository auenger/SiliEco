CREATE INDEX CONCURRENTLY user_composio_connection_user_status_idx ON public.user_composio_connection USING btree (user_id, status);
