CREATE UNIQUE INDEX CONCURRENTLY workflow_version_number_index ON public.workflow_version USING btree (workflow_id, version);
