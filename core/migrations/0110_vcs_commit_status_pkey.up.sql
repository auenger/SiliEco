CREATE UNIQUE INDEX CONCURRENTLY vcs_commit_status_pkey ON public.vcs_commit_status (connection_id, sha, context);
