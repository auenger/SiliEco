CREATE INDEX CONCURRENTLY idx_vcs_pull_request_connection ON public.vcs_pull_request USING btree (connection_id);
