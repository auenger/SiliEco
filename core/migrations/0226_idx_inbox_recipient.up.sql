CREATE INDEX CONCURRENTLY idx_inbox_recipient ON public.inbox_item USING btree (recipient_type, recipient_id, read);
