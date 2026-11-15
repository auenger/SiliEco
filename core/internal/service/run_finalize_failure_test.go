package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestFinalizeTaskClaimFailureRollsBackTokenThenRequeue is the regression for
// the PR #5193 review ask on the batch path's failure handling: when
// FinalizeTaskClaim fails (here: a delivery receipt referencing an id outside
// the task's comment plan, which the CAS subset-guard rejects), the token write
// is rolled back with it, and RequeueTaskAfterClaimFailure releases the exact
// dispatched claim back to `queued`. These are the two building blocks the
// batch handler composes on a finalization error.
func TestFinalizeRunClaimFailureRollsBackTokenThenRequeue(t *testing.T) {
	ctx := context.Background()
	pool := newRunClaimRacePool(t)
	svc := NewAgentRunService(db.New(pool), pool, nil, events.New())
	queries := db.New(pool)

	runID, userID, workspaceID := dispatchedCommentRunFixture(t, ctx, pool)
	agentRun, err := queries.GetAgentRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("load run: %v", err)
	}

	// A delivered id outside the task's plan (no trigger/coalesced match) makes
	// SetTaskDeliveredCommentIDs match zero rows → FinalizeTaskClaim errors.
	bogus := util.MustParseUUID("11111111-1111-1111-1111-111111111111")
	_, ferr := svc.FinalizeRunClaim(ctx, agentRun, db.CreateAgentRunTokenParams{
		TokenHash:   fmt.Sprintf("finalize-fail-hash-%d", time.Now().UnixNano()),
		RunID:       agentRun.ID,
		AgentID:     agentRun.AgentID,
		WorkspaceID: util.MustParseUUID(workspaceID),
		UserID:      util.MustParseUUID(userID),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
	}, []pgtype.UUID{bogus}, true)
	if ferr == nil {
		t.Fatal("expected FinalizeTaskClaim to fail for an out-of-plan delivery receipt")
	}
	if errors.Is(ferr, context.Canceled) {
		t.Fatalf("unexpected ctx error: %v", ferr)
	}

	// Token write must have rolled back with the receipt (transactional).
	var tokenCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_run_token WHERE run_id = $1`, runID).Scan(&tokenCount); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("expected token rolled back, found %d agent_run_token rows", tokenCount)
	}

	// The exact dispatched claim is released back to queued.
	if _, err := svc.RequeueRunAfterClaimFailure(ctx, agentRun); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want queued after requeue", status)
	}
}

// dispatchedCommentTaskFixture provisions a comment-backed task already in the
// `dispatched` state (never started), returning (runID, ownerUserID).
func dispatchedCommentRunFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (runID, userID, workspaceID string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Finalize Fail Test", fmt.Sprintf("finalize-fail-%d@silieco.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, task_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Finalize Fail Test", fmt.Sprintf("finalize-fail-%d", suffix), "temp finalize-fail test", "FFR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'daemon-ff', 'FF RT', 'cloud', 'ff_provider', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_runs, owner_id)
		VALUES ($1, 'FF Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 5, $3)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'ff task', 'in_progress', 'none', $2, 'member', 600001, 0)
		RETURNING id`, workspaceID, userID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'ff comment')
		RETURNING id`, workspaceID, taskID, userID).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, trigger_comment_id, status, priority, context, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'dispatched', 0, '{}'::jsonb, now(), NULL)
		RETURNING id`, agentID, runtimeID, taskID, commentID).Scan(&runID); err != nil {
		t.Fatalf("create dispatched run: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_run_token WHERE run_id = $1`, runID)
		pool.Exec(c, `DELETE FROM agent_run WHERE id = $1`, runID)
		pool.Exec(c, `DELETE FROM comment WHERE id = $1`, commentID)
		pool.Exec(c, `DELETE FROM task WHERE id = $1`, taskID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return runID, userID, workspaceID
}
