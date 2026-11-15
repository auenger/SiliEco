CREATE UNIQUE INDEX CONCURRENTLY vcs_connection_workspace_id_instance_url_key ON public.vcs_connection (workspace_id, instance_url);
