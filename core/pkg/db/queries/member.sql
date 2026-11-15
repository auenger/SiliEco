-- name: ListMembers :many
SELECT * FROM member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM member
WHERE user_id = $1 AND workspace_id = $2;

-- name: CreateMember :one
INSERT INTO member (workspace_id, user_id, role) SELECT
    $1 AS workspace_id,
    $2 AS user_id,
    $3 AS role
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($1) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_user_id WHERE ref_user_id.id = ($2) FOR KEY SHARE))

RETURNING *;

-- name: UpdateMemberRole :one
UPDATE member SET role = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMember :exec
WITH RECURSIVE cleanup_target_lark_user_binding AS MATERIALIZED (
    SELECT child.* FROM "lark_user_binding" child WHERE EXISTS (SELECT 1 FROM cleanup_target_member parent WHERE child."workspace_id" = parent."workspace_id" AND child."silieco_user_id" = parent."user_id")
),
cleanup_target_member AS MATERIALIZED (
    SELECT member.* FROM "member" member WHERE member.id = $1
),
cleanup_delete_lark_user_binding AS (
    DELETE FROM "lark_user_binding" child WHERE (child."id") IN (SELECT "id" FROM cleanup_target_lark_user_binding)
)
DELETE FROM "member" member WHERE (member."id") IN (SELECT "id" FROM cleanup_target_member) ;


-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;
