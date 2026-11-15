CREATE INDEX CONCURRENTLY attachment_reference_target_idx ON public.attachment_reference USING btree (workspace_id, target_type, target_id, created_at);
