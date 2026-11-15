CREATE INDEX CONCURRENTLY idx_lark_installation_lease ON public.lark_installation USING btree (ws_lease_expires_at) WHERE (status = 'active'::text);
