CREATE INDEX CONCURRENTLY skill_revision_listing_idx ON skill_revision(workspace_id,source_kind,source_id,created_at DESC,id DESC);
