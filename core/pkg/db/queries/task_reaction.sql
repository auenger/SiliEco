-- name: AddTaskReaction :one
INSERT INTO task_reaction (task_id, workspace_id, actor_type, actor_id, emoji) SELECT
    $1 AS task_id,
    $2 AS workspace_id,
    $3 AS actor_type,
    $4 AS actor_id,
    $5 AS emoji
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($2) FOR KEY SHARE))

ON CONFLICT (task_id, actor_type, actor_id, emoji) DO UPDATE SET created_at = task_reaction.created_at
RETURNING *;

-- name: RemoveTaskReaction :exec
DELETE FROM task_reaction
WHERE task_id = $1 AND actor_type = $2 AND actor_id = $3 AND emoji = $4;

-- name: ListTaskReactions :many
SELECT * FROM task_reaction
WHERE task_id = $1
ORDER BY created_at ASC;
