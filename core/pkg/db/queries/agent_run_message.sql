-- name: CreateAgentRunMessage :one
INSERT INTO agent_run_message (run_id, seq, type, tool, content, input, output) SELECT
    $1 AS run_id,
    $2 AS seq,
    $3 AS type,
    $4 AS tool,
    $5 AS content,
    $6 AS input,
    $7 AS output
WHERE (($1)::uuid IS NULL OR EXISTS (SELECT 1 FROM agent_run AS ref_run_id WHERE ref_run_id.id = ($1) FOR KEY SHARE))

RETURNING *;

-- name: ListAgentRunMessages :many
SELECT * FROM agent_run_message
WHERE run_id = $1
ORDER BY seq ASC;

-- name: ListAgentRunMessagesSince :many
SELECT * FROM agent_run_message
WHERE run_id = $1 AND seq > $2
ORDER BY seq ASC;

-- name: DeleteAgentRunMessages :exec
DELETE FROM agent_run_message
WHERE run_id = $1;
