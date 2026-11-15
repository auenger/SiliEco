CREATE INDEX CONCURRENTLY idx_github_pr_check_suite_aggregate ON public.github_pull_request_check_suite USING btree (pr_id, head_sha, app_id, updated_at DESC);
