CREATE INDEX CONCURRENTLY idx_attachment_workspace ON public.attachment USING btree (workspace_id);
