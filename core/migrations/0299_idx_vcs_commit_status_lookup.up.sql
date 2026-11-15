CREATE INDEX CONCURRENTLY idx_vcs_commit_status_lookup ON public.vcs_commit_status USING btree (connection_id, sha);
