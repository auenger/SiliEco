CREATE UNIQUE INDEX CONCURRENTLY attachment_reference_target_uidx ON public.attachment_reference USING btree (workspace_id, attachment_id, target_type, target_id);
