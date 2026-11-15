CREATE UNIQUE INDEX CONCURRENTLY vcs_pull_request_connection_id_repo_owner_repo_name_pr_numb_key ON public.vcs_pull_request (connection_id, repo_owner, repo_name, pr_number);
