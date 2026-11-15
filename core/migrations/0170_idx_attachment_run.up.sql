CREATE INDEX CONCURRENTLY idx_attachment_run ON public.attachment USING btree (run_id) WHERE (run_id IS NOT NULL);
