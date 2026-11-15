-- name: CreateAttachment :one
INSERT INTO attachment (
  id, workspace_id, task_id, comment_id, chat_session_id, run_id,
  uploader_type, uploader_id, filename, url, content_type, size_bytes
) SELECT
    $1 AS id,
    $2 AS workspace_id,
    sqlc.narg(task_id) AS task_id,
    sqlc.narg(comment_id) AS comment_id,
    sqlc.narg(chat_session_id) AS chat_session_id,
    sqlc.narg(run_id) AS run_id,
    $3 AS uploader_type,
    $4 AS uploader_id,
    $5 AS filename,
    $6 AS url,
    $7 AS content_type,
    $8 AS size_bytes
WHERE (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($2) FOR KEY SHARE))
  AND ((sqlc.narg(task_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = (sqlc.narg(task_id)) FOR KEY SHARE))
  AND ((sqlc.narg(comment_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM comment AS ref_comment_id WHERE ref_comment_id.id = (sqlc.narg(comment_id)) FOR KEY SHARE))
  AND ((sqlc.narg(chat_session_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_session AS ref_chat_session_id WHERE ref_chat_session_id.id = (sqlc.narg(chat_session_id)) FOR KEY SHARE))

RETURNING *;

-- name: ListAttachmentsByTask :many
SELECT a.* FROM attachment a
WHERE a.workspace_id = $2
  AND (
      a.task_id = $1
      OR EXISTS (
          SELECT 1 FROM attachment_reference ar
          WHERE ar.workspace_id = a.workspace_id
            AND ar.attachment_id = a.id
            AND ar.target_type = 'task'
            AND ar.target_id = $1
      )
  )
ORDER BY a.created_at ASC;

-- name: ListAttachmentsByComment :many
SELECT * FROM attachment
WHERE comment_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: GetAttachment :one
SELECT * FROM attachment
WHERE id = $1 AND workspace_id = $2;

-- name: GetAttachmentByIDOnly :one
-- Used by the download endpoint, which derives workspace context from the
-- attachment row itself rather than from request headers/query params. The
-- caller still has to verify the requester is a member of the returned
-- workspace_id before serving the bytes — this query is access-neutral on
-- purpose so a self-contained URL like /api/attachments/{id}/download can
-- work as a native <img>/<video> resource load (no header attachment).
SELECT * FROM attachment
WHERE id = $1;

-- name: ListAttachmentsByCommentIDs :many
SELECT * FROM attachment
WHERE comment_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentURLsByTaskOrComments :many
SELECT a.url FROM attachment a
WHERE a.task_id = $1
   OR a.comment_id IN (SELECT c.id FROM comment c WHERE c.task_id = $1);

-- name: ListAttachmentURLsByCommentID :many
SELECT url FROM attachment
WHERE comment_id = $1;

-- name: LinkAttachmentsToComment :exec
UPDATE attachment
SET comment_id = $1
WHERE attachment.task_id = $2
  AND attachment.comment_id IS NULL
  AND attachment.id = ANY($3::uuid[])
  AND (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM comment AS ref_comment_id WHERE ref_comment_id.id = ($1) FOR KEY SHARE))
;

-- name: ReplaceCommentAttachments :exec
UPDATE attachment
SET comment_id = CASE
  WHEN id = ANY(sqlc.arg(attachment_ids)::uuid[]) THEN $1
  ELSE NULL
END
WHERE attachment.task_id = $2
  AND (
    attachment.comment_id = $1
    OR (attachment.comment_id IS NULL AND attachment.id = ANY(sqlc.arg(attachment_ids)::uuid[]))
  )
  AND (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM comment AS ref_comment_id WHERE ref_comment_id.id = ($1) FOR KEY SHARE))
;

-- name: LinkAttachmentsToChatMessage :many
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id),
    chat_session_id = sqlc.arg(chat_session_id)
WHERE attachment.workspace_id = sqlc.arg(workspace_id)
  AND attachment.task_id IS NULL
  AND attachment.comment_id IS NULL
  AND attachment.chat_message_id IS NULL
  AND (
    attachment.chat_session_id IS NULL
    OR attachment.chat_session_id = sqlc.arg(chat_session_id)
  )
  AND uploader_type = sqlc.arg(uploader_type)
  AND uploader_id = sqlc.arg(uploader_id)
  AND attachment.id = ANY(sqlc.arg(attachment_ids)::uuid[])

  AND ((sqlc.arg(chat_session_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_session AS ref_chat_session_id WHERE ref_chat_session_id.id = (sqlc.arg(chat_session_id)) FOR KEY SHARE))
  AND ((sqlc.arg(chat_message_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_message AS ref_chat_message_id WHERE ref_chat_message_id.id = (sqlc.arg(chat_message_id)) FOR KEY SHARE))
RETURNING attachment.id;

-- name: DetachAttachmentsFromUserChatMessageByRun :many
-- When an empty chat run is cancelled, its user message is deleted. The
-- attachment FK is ON DELETE CASCADE, so without this the bound rows would be
-- destroyed and a restored draft could never re-bind them. Detach first
-- (chat_message_id -> NULL, keep chat_session_id) so the rows survive as
-- workspace/session-scoped unattached attachments and re-send can re-link them.
UPDATE attachment
SET chat_message_id = NULL
WHERE chat_message_id IN (
  SELECT id FROM chat_message WHERE chat_message.run_id = $1 AND role = 'user'
)
RETURNING *;

-- name: CountUnboundChatAttachmentsForRun :one
-- How many attachments the agent produced for this chat run that are still
-- unbound to any owner. Lets CompleteRun create an assistant message (and
-- bind them) even when the agent's text output was empty but it uploaded files.
SELECT COUNT(*) FROM attachment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND run_id = sqlc.arg(run_id)
  AND task_id IS NULL
  AND comment_id IS NULL
  AND chat_message_id IS NULL;

-- name: BindChatAttachmentsToMessage :many
-- Bind a chat agent's run-scoped attachments to the assistant reply it just
-- produced. Only rows still unclaimed by any owner (task/comment/chat_message)
-- are eligible, so an attachment already linked elsewhere is never stolen.
-- Returns the bound ids for logging.
UPDATE attachment
SET chat_message_id = sqlc.arg(chat_message_id)
WHERE attachment.workspace_id = sqlc.arg(workspace_id)
  AND attachment.run_id = sqlc.arg(run_id)
  AND attachment.task_id IS NULL
  AND attachment.comment_id IS NULL
  AND attachment.chat_message_id IS NULL

  AND ((sqlc.arg(chat_message_id))::uuid IS NULL OR EXISTS (SELECT 1 FROM chat_message AS ref_chat_message_id WHERE ref_chat_message_id.id = (sqlc.arg(chat_message_id)) FOR KEY SHARE))
RETURNING attachment.id;

-- name: ListAttachmentsByChatMessage :many
SELECT * FROM attachment
WHERE chat_message_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListAttachmentsByChatMessageIDs :many
SELECT * FROM attachment
WHERE chat_message_id = ANY($1::uuid[]) AND workspace_id = $2
ORDER BY created_at ASC;

-- name: LinkAttachmentsToTask :exec
UPDATE attachment
SET task_id = $1
WHERE attachment.workspace_id = $2
  AND attachment.task_id IS NULL
  AND attachment.id = ANY($3::uuid[])
  AND (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM task AS ref_task_id WHERE ref_task_id.id = ($1) FOR KEY SHARE))
;

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE attachment.id = $1 AND workspace_id = $2;

-- name: ListAttachmentsByIDs :many
SELECT * FROM attachment
WHERE id = ANY(sqlc.arg(attachment_ids)::uuid[]) AND workspace_id = sqlc.arg(workspace_id)
ORDER BY created_at ASC;
