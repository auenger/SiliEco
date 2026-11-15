CREATE UNIQUE INDEX CONCURRENTLY workflow_review_recovery_source_idx ON public.workflow_review_recovery USING btree (workspace_id, failed_run_id);
