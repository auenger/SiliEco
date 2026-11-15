package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// TestClaimTasksForRuntimes_PromotesDeferredAndEmitsQueuedEvent is the
// regression test for the PR #5193 review must-fix: the batch promote step must
// replay the singular service method's per-row side effects — emit the
// deferred→queued (`run:queued`) event AND invalidate the empty-claim verdict
// via NotifyTaskEnqueued — so a just-promoted deferred task is claimable in the
// SAME batch call rather than sitting idle until the empty key's TTL.
//
// The empty-claim cache is nil in tests (no Redis), so this asserts the two
// directly observable halves of the fix: (1) the promoted task is claimed this
// round, and (2) a `run:queued` event is published for it.
func TestClaimRunsForRuntimes_PromotesDeferredAndEmitsQueuedEvent(t *testing.T) {
	ctx := context.Background()
	pool := newRunClaimRacePool(t)
	bus := events.New()

	var mu sync.Mutex
	queuedRunIDs := map[string]int{}
	bus.Subscribe(protocol.EventRunQueued, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			if id, ok := payload["run_id"].(string); ok {
				mu.Lock()
				queuedRunIDs[id]++
				mu.Unlock()
			}
		}
	})

	svc := NewAgentRunService(db.New(pool), pool, nil, bus)
	rt, deferredRunID := deferredBatchFixture(t, ctx, pool)

	got, err := svc.ClaimRunsForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(rt)}, 5)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 1 || util.UUIDToString(got[0].ID) != deferredRunID {
		t.Fatalf("expected the promoted deferred task %s to be claimed this round, got %d tasks", deferredRunID, len(got))
	}

	mu.Lock()
	n := queuedRunIDs[deferredRunID]
	mu.Unlock()
	if n != 1 {
		t.Fatalf("expected exactly one run:queued event for the promoted task, got %d", n)
	}
}

// deferredBatchFixture provisions one runtime + agent with a single DEFERRED
// task whose fire_at is already due. Returns the runtime id and the task id.
func deferredBatchFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (runtimeID, runID string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Deferred Batch Test", fmt.Sprintf("deferred-batch-%d@silieco.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, task_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Deferred Batch Test", fmt.Sprintf("deferred-batch-%d", suffix), "temp deferred batch test", "DBR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'daemon-deferred', 'Deferred RT', 'cloud', 'deferred_provider', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_runs, owner_id)
		VALUES ($1, 'Deferred Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 5, $3)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'deferred task', 'in_progress', 'none', $2, 'member', 700001, 0)
		RETURNING id`, workspaceID, userID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, task_id, status, priority, context, runtime_id, fire_at)
		VALUES ($1, $2, 'deferred', 0, '{}'::jsonb, $3, now() - interval '1 minute')
		RETURNING id`, agentID, taskID, runtimeID).Scan(&runID); err != nil {
		t.Fatalf("create deferred run: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_run WHERE agent_id = $1`, agentID)
		pool.Exec(c, `DELETE FROM task WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return runtimeID, runID
}
