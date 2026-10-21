-- name: CreateSkillRevision :one
INSERT INTO skill_revision(workspace_id,source_kind,source_id,revision_hash,bundle,manifest,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(workspace_id,source_kind,source_id,revision_hash) DO NOTHING
RETURNING *;

-- name: GetSkillRevisionInWorkspace :one
SELECT * FROM skill_revision WHERE workspace_id=$1 AND id=$2;

-- name: GetSkillRevisionByHash :one
SELECT * FROM skill_revision WHERE workspace_id=$1 AND source_kind=$2 AND source_id=$3 AND revision_hash=$4;

-- name: LockWorkspaceForSkillRevisionCreate :one
-- Shares the workspace delete/create lock protocol; no foreign key is assumed.
SELECT id FROM workspace WHERE id=$1 FOR KEY SHARE;

-- name: ListSkillRevisionSummaries :many
SELECT id,workspace_id,source_kind,source_id,revision_hash,manifest,created_by,created_at
FROM skill_revision
WHERE workspace_id=sqlc.arg(workspace_id) AND source_kind=sqlc.arg(source_kind) AND source_id=sqlc.arg(source_id)
  AND (sqlc.narg(after_created_at)::timestamptz IS NULL OR (created_at,id)<(sqlc.narg(after_created_at)::timestamptz,sqlc.narg(after_id)::uuid))
ORDER BY created_at DESC,id DESC
LIMIT sqlc.arg(page_limit);

-- name: GetSkillRevisionCursorInWorkspace :one
SELECT id,source_kind,source_id,created_at FROM skill_revision WHERE workspace_id=$1 AND id=$2;
