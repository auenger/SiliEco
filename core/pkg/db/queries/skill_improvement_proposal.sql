-- name: CreateSkillImprovementProposal :one
INSERT INTO skill_improvement_proposal(workspace_id,skill_id,baseline_revision_id,candidate_revision_id,request_id,request_hash,category,reason,hypothesis,applicable_scope,evidence,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(workspace_id,created_by,request_id) DO NOTHING RETURNING *;

-- name: GetSkillImprovementProposalByRequest :one
SELECT * FROM skill_improvement_proposal WHERE workspace_id=$1 AND created_by=$2 AND request_id=$3;

-- name: GetSkillImprovementProposal :one
SELECT * FROM skill_improvement_proposal WHERE workspace_id=$1 AND skill_id=$2 AND id=$3;

-- name: ListSkillImprovementProposals :many
SELECT * FROM skill_improvement_proposal
WHERE workspace_id=sqlc.arg(workspace_id) AND skill_id=sqlc.arg(skill_id)
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL OR (created_at,id)<(sqlc.narg(after_created_at)::timestamptz,sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC,id DESC LIMIT sqlc.arg(page_limit);
