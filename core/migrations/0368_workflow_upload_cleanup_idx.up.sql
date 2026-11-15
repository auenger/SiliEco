CREATE INDEX CONCURRENTLY workflow_upload_cleanup_idx ON public.workflow_artifact_upload USING btree (status, expires_at);
