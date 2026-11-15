-- name: CreateFeedback :one
INSERT INTO feedback (user_id, workspace_id, message, metadata) SELECT
    $1 AS user_id,
    sqlc.narg(workspace_id) AS workspace_id,
    $2 AS message,
    $3 AS metadata
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_user_id WHERE ref_user_id.id = ($1) FOR KEY SHARE))
  AND ((sqlc.narg(workspace_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = (sqlc.narg(workspace_id)) FOR KEY SHARE))

RETURNING *;

-- name: CountRecentFeedbackByUser :one
SELECT count(*) FROM feedback
WHERE user_id = $1 AND created_at > now() - interval '1 hour';
