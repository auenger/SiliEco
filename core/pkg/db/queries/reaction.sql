-- name: AddReaction :one
INSERT INTO comment_reaction (comment_id, workspace_id, actor_type, actor_id, emoji) SELECT
    $1 AS comment_id,
    $2 AS workspace_id,
    $3 AS actor_type,
    $4 AS actor_id,
    $5 AS emoji
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM comment AS ref_comment_id WHERE ref_comment_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($2) FOR KEY SHARE))

ON CONFLICT (comment_id, actor_type, actor_id, emoji) DO UPDATE SET created_at = comment_reaction.created_at
RETURNING *;

-- name: RemoveReaction :exec
DELETE FROM comment_reaction
WHERE comment_id = $1 AND actor_type = $2 AND actor_id = $3 AND emoji = $4;

-- name: ListReactionsByCommentIDs :many
SELECT * FROM comment_reaction
WHERE comment_id = ANY($1::uuid[])
ORDER BY created_at ASC;
