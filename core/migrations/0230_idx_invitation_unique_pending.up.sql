CREATE UNIQUE INDEX CONCURRENTLY idx_invitation_unique_pending ON public.workspace_invitation USING btree (workspace_id, invitee_email) WHERE (status = 'pending'::text);
