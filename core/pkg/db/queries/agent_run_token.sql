-- name: CreateAgentRunToken :one
INSERT INTO agent_run_token (token_hash, run_id, agent_id, workspace_id, user_id, expires_at) SELECT
    $1 AS token_hash,
    $2 AS run_id,
    $3 AS agent_id,
    $4 AS workspace_id,
    $5 AS user_id,
    $6 AS expires_at
WHERE (($5)::uuid IS NULL OR EXISTS (SELECT 1 FROM "user" AS ref_user_id WHERE ref_user_id.id = ($5) FOR KEY SHARE))
  AND (($2)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_run AS ref_run_id WHERE ref_run_id.id = ($2) FOR KEY SHARE))
  AND (($3)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent AS ref_agent_id WHERE ref_agent_id.id = ($3) FOR KEY SHARE))
  AND (($4)::uuid IS NULL OR EXISTS (SELECT 1 FROM workspace AS ref_workspace_id WHERE ref_workspace_id.id = ($4) FOR KEY SHARE))

RETURNING *;

-- name: GetAgentRunTokenByHash :one
SELECT * FROM agent_run_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteAgentRunTokensByRun :exec
DELETE FROM agent_run_token WHERE run_id = $1;

-- name: DeleteExpiredAgentRunTokens :exec
DELETE FROM agent_run_token WHERE expires_at <= now();
