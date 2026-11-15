CREATE INDEX CONCURRENTLY idx_task_pull_request_pr ON public.task_pull_request USING btree (pull_request_id);
