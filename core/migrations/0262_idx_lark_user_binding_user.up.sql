CREATE INDEX CONCURRENTLY idx_lark_user_binding_user ON public.lark_user_binding USING btree (silieco_user_id, workspace_id);
