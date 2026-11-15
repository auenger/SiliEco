package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// batchClaimFixture provisions two runtimes on one machine, each with its own
// agent (max_concurrent_runs=5): agent1 (on rt1) has two queued tasks on two
// different tasks; agent2 (on rt2) has one queued task. Returns the two
// runtime IDs.
func batchClaimFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (rt1, rt2 string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Batch Claim Test", fmt.Sprintf("batch-claim-%d@silieco.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, task_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Batch Claim Test", fmt.Sprintf("batch-claim-%d", suffix), "temp batch claim test workspace", "BCR").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}

	mkRuntime := func(name, provider string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
			VALUES ($1, 'daemon-batch', $2, 'cloud', $3, 'online', 'test runtime', '{}'::jsonb, now(), 'private', $4)
			RETURNING id`, workspaceID, name, provider, userID).Scan(&id); err != nil {
			t.Fatalf("create runtime %s: %v", name, err)
		}
		return id
	}
	mkAgent := func(name, runtimeID string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_runs, owner_id)
			VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 5, $4)
			RETURNING id`, workspaceID, name, runtimeID, userID).Scan(&id); err != nil {
			t.Fatalf("create agent %s: %v", name, err)
		}
		return id
	}

	rt1 = mkRuntime("Batch RT1", "batch_provider_1")
	rt2 = mkRuntime("Batch RT2", "batch_provider_2")
	agent1 := mkAgent("Batch Agent1", rt1)
	agent2 := mkAgent("Batch Agent2", rt2)

	mkQueuedRun := func(agentID, runtimeID string, n int) {
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position)
			VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, $5)
			RETURNING id`, workspaceID, fmt.Sprintf("batch task %d", n), userID, 800000+n, n).Scan(&taskID); err != nil {
			t.Fatalf("create task %d: %v", n, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO agent_run (agent_id, task_id, status, priority, context, runtime_id)
			VALUES ($1, $2, 'queued', 0, '{}'::jsonb, $3)`, agentID, taskID, runtimeID); err != nil {
			t.Fatalf("create task %d: %v", n, err)
		}
		// Space out created_at so ordering is deterministic.
		time.Sleep(2 * time.Millisecond)
	}
	mkQueuedRun(agent1, rt1, 1)
	mkQueuedRun(agent1, rt1, 2)
	mkQueuedRun(agent2, rt2, 3)

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_run WHERE agent_id IN ($1,$2)`, agent1, agent2)
		pool.Exec(c, `DELETE FROM task WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM agent WHERE id IN ($1,$2)`, agent1, agent2)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id IN ($1,$2)`, rt1, rt2)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return rt1, rt2
}

// TestClaimTasksForRuntimes_MultiRuntimeDrain verifies the machine-level batch
// claim (SILI-4257): a single call claims across all runtimes, one task per
// agent per call (matching the singular path's dedup), routes each task to its
// runtime, respects a subsequent drain, and reports empty once nothing is
// queued.
func TestClaimRunsForRuntimes_MultiRuntimeDrain(t *testing.T) {
	ctx := context.Background()
	pool := newRunClaimRacePool(t)
	svc := NewAgentRunService(db.New(pool), pool, nil, events.New())

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}

	// Call 1: one task per agent (agent1→rt1, agent2→rt2) => 2 tasks, one per runtime.
	got1, err := svc.ClaimRunsForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("call1: %v", err)
	}
	if len(got1) != 2 {
		t.Fatalf("call1 claimed %d tasks, want 2", len(got1))
	}
	seen := map[string]int{}
	for _, agentRun := range got1 {
		seen[util.UUIDToString(agentRun.RuntimeID)]++
	}
	if seen[rt1] != 1 || seen[rt2] != 1 {
		t.Fatalf("call1 runtime distribution = %v, want one task each for rt1/rt2", seen)
	}

	// Call 2: agent1 still has a second queued task (different task, capacity 5);
	// agent2 is drained => exactly 1 task, on rt1.
	got2, err := svc.ClaimRunsForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("call2: %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("call2 claimed %d tasks, want 1", len(got2))
	}
	if util.UUIDToString(got2[0].RuntimeID) != rt1 {
		t.Fatalf("call2 claimed runtime = %s, want rt1", util.UUIDToString(got2[0].RuntimeID))
	}

	// Call 3: everything dispatched => empty.
	got3, err := svc.ClaimRunsForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("call3: %v", err)
	}
	if len(got3) != 0 {
		t.Fatalf("call3 claimed %d tasks, want 0", len(got3))
	}
}

// TestClaimTasksForRuntimes_MaxTasksCap verifies max_runs bounds the number of
// tasks returned in a single batch call.
func TestClaimRunsForRuntimes_MaxRunsCap(t *testing.T) {
	ctx := context.Background()
	pool := newRunClaimRacePool(t)
	svc := NewAgentRunService(db.New(pool), pool, nil, events.New())

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}

	got, err := svc.ClaimRunsForRuntimes(ctx, ids, 1)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("claimed %d tasks with max_runs=1, want 1", len(got))
	}
}

// TestClaimTasksForRuntimes_EmptyInputs guards the trivial short-circuits.
func TestClaimRunsForRuntimes_EmptyInputs(t *testing.T) {
	ctx := context.Background()
	pool := newRunClaimRacePool(t)
	svc := NewAgentRunService(db.New(pool), pool, nil, events.New())

	if got, err := svc.ClaimRunsForRuntimes(ctx, nil, 5); err != nil || got != nil {
		t.Fatalf("nil runtimes: got=%v err=%v, want nil,nil", got, err)
	}
	rt1, _ := batchClaimFixture(t, ctx, pool)
	if got, err := svc.ClaimRunsForRuntimes(ctx, []pgtype.UUID{util.MustParseUUID(rt1)}, 0); err != nil || got != nil {
		t.Fatalf("maxTasks=0: got=%v err=%v, want nil,nil", got, err)
	}
}
