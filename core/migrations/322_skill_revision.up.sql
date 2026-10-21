CREATE TABLE skill_revision (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('workspace','builtin','runtime')),
    source_id TEXT NOT NULL CHECK (length(btrim(source_id)) > 0),
    revision_hash TEXT NOT NULL CHECK (revision_hash ~ '^sha256:[0-9a-f]{64}$'),
    bundle JSONB NOT NULL CHECK (jsonb_typeof(bundle) = 'object'),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE FUNCTION reject_skill_revision_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'skill revisions are immutable' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER skill_revision_immutable BEFORE UPDATE ON skill_revision
FOR EACH ROW EXECUTE FUNCTION reject_skill_revision_update();
