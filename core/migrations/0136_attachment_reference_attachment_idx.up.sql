CREATE INDEX CONCURRENTLY attachment_reference_attachment_idx ON public.attachment_reference USING btree (workspace_id, attachment_id, created_at);
