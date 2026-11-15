CREATE INDEX CONCURRENTLY idx_inbox_recipient_archived_created ON public.inbox_item USING btree (workspace_id, recipient_type, recipient_id, archived, created_at DESC);
