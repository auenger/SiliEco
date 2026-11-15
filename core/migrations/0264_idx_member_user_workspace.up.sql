CREATE INDEX CONCURRENTLY idx_member_user_workspace ON public.member USING btree (user_id, workspace_id);
