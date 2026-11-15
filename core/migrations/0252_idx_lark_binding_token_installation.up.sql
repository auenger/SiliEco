CREATE INDEX CONCURRENTLY idx_lark_binding_token_installation ON public.lark_binding_token USING btree (installation_id, expires_at);
