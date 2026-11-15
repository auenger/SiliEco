CREATE UNIQUE INDEX CONCURRENTLY squad_member_squad_id_member_type_member_id_key ON public.squad_member (squad_id, member_type, member_id);
