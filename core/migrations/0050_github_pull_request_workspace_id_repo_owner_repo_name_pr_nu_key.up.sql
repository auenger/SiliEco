CREATE UNIQUE INDEX CONCURRENTLY github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key ON public.github_pull_request (workspace_id, repo_owner, repo_name, pr_number);
