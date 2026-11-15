-- name: ListTaskProperties :many
-- usage_count = number of tasks in the workspace that currently carry a
-- value for this property. `properties ? id` is a seq scan today; fine at
-- the 20-definition / small-workspace scale this feature targets.
SELECT p.*,
    (
        SELECT COUNT(*) FROM task i
        WHERE i.workspace_id = p.workspace_id
          AND i.properties ? p.id::text
    )::bigint AS usage_count
FROM task_property p
WHERE p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR p.archived_at IS NULL)
ORDER BY p.position ASC, LOWER(p.name) ASC;

-- name: GetTaskProperty :one
SELECT * FROM task_property
WHERE id = $1 AND workspace_id = $2;

-- name: CountActiveTaskProperties :one
SELECT COUNT(*) FROM task_property
WHERE workspace_id = $1 AND archived_at IS NULL;

-- name: CreateTaskProperty :one
-- New definitions append to the end of the catalog: position = max + 1.
INSERT INTO task_property (workspace_id, name, type, description, icon, config, position)
SELECT sqlc.arg('workspace_id')::uuid,
       sqlc.arg('name')::text,
       sqlc.arg('type')::text,
       sqlc.arg('description')::text,
       sqlc.arg('icon')::text,
       sqlc.arg('config')::jsonb,
       COALESCE((SELECT MAX(position) FROM task_property WHERE task_property.workspace_id = sqlc.arg('workspace_id')::uuid), 0) + 1
RETURNING *;

-- name: UpdateTaskProperty :one
-- `type` is deliberately immutable — changing it would silently invalidate
-- existing values. archived_set/archived_at implement tri-state semantics:
-- archived_set=false leaves archived_at untouched.
UPDATE task_property SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    config = COALESCE(sqlc.narg('config'), config),
    archived_at = CASE WHEN sqlc.arg('archived_set')::bool THEN sqlc.narg('archived_at') ELSE archived_at END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetTaskPropertyValue :one
-- Single-key atomic write (mirror of SetTaskMetadataKey): concurrent writers
-- on different property keys never clobber each other.
UPDATE task
SET properties = jsonb_set(properties, ARRAY[sqlc.arg('key')::text], sqlc.arg('value')::jsonb, true),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: DeleteTaskPropertyValue :one
UPDATE task
SET properties = properties - sqlc.arg('key')::text,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: CountTasksUsingPropertyOptions :many
-- Usage census for specific option ids of one property. jsonb `?` matches
-- both value shapes: array element for multi_select, string equality for
-- select. Only options with at least one referencing task come back.
SELECT opt::text AS option_id, COUNT(i.id) AS usage_count
FROM unnest(sqlc.arg('option_ids')::text[]) AS opt
LEFT JOIN task i
  ON i.workspace_id = sqlc.arg('workspace_id')::uuid
 AND (i.properties -> sqlc.arg('property_key')::text) ? opt
GROUP BY opt
HAVING COUNT(i.id) > 0;
