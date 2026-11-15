-- name: GetNotificationPreference :one
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, preferences) SELECT
    $1 AS workspace_id,
    $2 AS user_id,
    $3 AS preferences
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_user_id WHERE ref_user_id.id = ($2) FOR KEY SHARE))

ON CONFLICT (workspace_id, user_id)
DO UPDATE SET preferences = $3, updated_at = now()
RETURNING *;

-- name: PatchNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, preferences) SELECT
    $1 AS workspace_id,
    $2 AS user_id,
    $3 AS preferences
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_user_id WHERE ref_user_id.id = ($2) FOR KEY SHARE))

ON CONFLICT (workspace_id, user_id)
DO UPDATE SET
    preferences = notification_preference.preferences || EXCLUDED.preferences,
    updated_at = now()
RETURNING *;

-- name: ListNotificationPreferencesByUsers :many
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = ANY(sqlc.arg('user_ids')::uuid[]);
