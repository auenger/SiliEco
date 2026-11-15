CREATE INDEX CONCURRENTLY idx_lark_user_binding_workspace_open ON public.lark_user_binding USING btree (workspace_id, lark_open_id);
