CREATE UNIQUE INDEX CONCURRENTLY github_pull_request_check_run_pr_ordinal_idx ON public.github_pull_request_check_run USING btree (pr_id, ordinal);
