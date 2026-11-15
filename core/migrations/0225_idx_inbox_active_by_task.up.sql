CREATE INDEX CONCURRENTLY idx_inbox_active_by_task ON public.inbox_item USING btree (workspace_id, recipient_type, recipient_id, task_id) WHERE (archived = false);
