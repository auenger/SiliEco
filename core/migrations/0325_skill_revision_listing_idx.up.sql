CREATE INDEX CONCURRENTLY skill_revision_listing_idx ON public.skill_revision USING btree (workspace_id, source_kind, source_id, created_at DESC, id DESC);
