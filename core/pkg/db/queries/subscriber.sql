-- name: AddTaskSubscriber :exec
INSERT INTO task_subscriber (task_id, user_type, user_id, reason) SELECT
    $1 AS task_id,
    $2 AS user_type,
    $3 AS user_id,
    $4 AS reason
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($1) FOR KEY SHARE))

ON CONFLICT (task_id, user_type, user_id) DO NOTHING;

-- name: RemoveTaskSubscriber :exec
DELETE FROM task_subscriber
WHERE task_id = $1 AND user_type = $2 AND user_id = $3;

-- name: ListTaskSubscribers :many
SELECT * FROM task_subscriber
WHERE task_id = $1
ORDER BY created_at;

-- name: IsTaskSubscriber :one
SELECT EXISTS(
    SELECT 1 FROM task_subscriber
    WHERE task_id = $1 AND user_type = $2 AND user_id = $3
) AS subscribed;
