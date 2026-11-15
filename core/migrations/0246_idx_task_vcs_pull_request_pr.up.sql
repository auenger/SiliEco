CREATE INDEX CONCURRENTLY idx_task_vcs_pull_request_pr ON public.task_vcs_pull_request USING btree (pull_request_id);
