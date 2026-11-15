CREATE INDEX CONCURRENTLY idx_pinned_item_user_ws ON public.pinned_item USING btree (workspace_id, user_id, "position");
