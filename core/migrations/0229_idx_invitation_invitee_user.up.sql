CREATE INDEX CONCURRENTLY idx_invitation_invitee_user ON public.workspace_invitation USING btree (invitee_user_id) WHERE (status = 'pending'::text);
