CREATE TABLE skill_evaluation_suite (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    skill_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),
    suite_hash TEXT NOT NULL CHECK (suite_hash ~ '^sha256:[0-9a-f]{64}$'),
    schema_version TEXT NOT NULL,
    validator_version TEXT NOT NULL,
    definition BYTEA NOT NULL CHECK (octet_length(definition) BETWEEN 1 AND 8388608),
    improvement_count INTEGER NOT NULL CHECK (improvement_count >= 0),
    regression_count INTEGER NOT NULL CHECK (regression_count >= 0),
    held_out_count INTEGER NOT NULL CHECK (held_out_count >= 0),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (improvement_count + regression_count + held_out_count BETWEEN 1 AND 1000)
);

CREATE FUNCTION reject_skill_evaluation_suite_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'skill evaluation suites are immutable' USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER skill_evaluation_suite_immutable
BEFORE UPDATE ON skill_evaluation_suite
FOR EACH ROW EXECUTE FUNCTION reject_skill_evaluation_suite_update();
