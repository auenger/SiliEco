CREATE UNIQUE INDEX CONCURRENTLY skill_revision_content_idx ON public.skill_revision USING btree (workspace_id, source_kind, source_id, revision_hash);
