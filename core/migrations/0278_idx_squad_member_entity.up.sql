CREATE INDEX CONCURRENTLY idx_squad_member_entity ON public.squad_member USING btree (member_type, member_id);
