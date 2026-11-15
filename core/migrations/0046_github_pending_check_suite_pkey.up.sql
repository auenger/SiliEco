CREATE UNIQUE INDEX CONCURRENTLY github_pending_check_suite_pkey ON public.github_pending_check_suite (workspace_id, repo_owner, repo_name, pr_number, suite_id);
