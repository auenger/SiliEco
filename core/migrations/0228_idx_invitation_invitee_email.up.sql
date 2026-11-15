CREATE INDEX CONCURRENTLY idx_invitation_invitee_email ON public.workspace_invitation USING btree (invitee_email) WHERE (status = 'pending'::text);
