CREATE UNIQUE INDEX CONCURRENTLY pinned_item_workspace_id_user_id_item_type_item_id_key ON public.pinned_item (workspace_id, user_id, item_type, item_id);
