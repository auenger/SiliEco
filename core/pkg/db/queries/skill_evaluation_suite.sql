-- name: CreateSkillEvaluationSuite :one
INSERT INTO skill_evaluation_suite(workspace_id,skill_id,name,suite_hash,schema_version,validator_version,definition,improvement_count,regression_count,held_out_count,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT(workspace_id,skill_id,suite_hash) DO NOTHING RETURNING *;

-- name: GetSkillEvaluationSuiteByHash :one
SELECT * FROM skill_evaluation_suite WHERE workspace_id=$1 AND skill_id=$2 AND suite_hash=$3;

-- name: GetSkillEvaluationSuite :one
-- Private definition: use only through evaluation service authorization.
SELECT * FROM skill_evaluation_suite WHERE workspace_id=$1 AND skill_id=$2 AND id=$3;

-- name: ListSkillEvaluationSuiteSummaries :many
-- Intentionally omits the definition, which contains held-out inputs and answers.
SELECT id,workspace_id,skill_id,name,suite_hash,schema_version,validator_version,improvement_count,regression_count,held_out_count,created_by,created_at
FROM skill_evaluation_suite
WHERE workspace_id=sqlc.arg(workspace_id) AND skill_id=sqlc.arg(skill_id)
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL OR (created_at,id)<(sqlc.narg(after_created_at)::timestamptz,sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit);

-- name: GetSkillEvaluationSuiteCursor :one
SELECT id,created_at FROM skill_evaluation_suite WHERE workspace_id=$1 AND skill_id=$2 AND id=$3;
