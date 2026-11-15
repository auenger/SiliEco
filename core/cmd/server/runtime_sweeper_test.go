package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/silieco-ai/silieco/core/internal/events"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// setupSweeperTestFixture creates an task and a task in the given status with
// timestamps old enough to trigger the sweeper. Returns (taskID, agentID, runID).
func setupSweeperTestFixture(t *testing.T, runStatus string) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	// Find the integration test agent
	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create an task assigned to the agent
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Sweeper test task', 'todo', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	// Create a task in the desired status with old timestamps
	var runID string
	switch runStatus {
	case "running":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, dispatched_at, started_at)
			VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
			RETURNING id
		`, agentID, runtimeID, taskID).Scan(&runID)
	case "dispatched":
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, dispatched_at)
			VALUES ($1, $2, $3, 'dispatched', 0, now() - interval '10 minutes')
			RETURNING id
		`, agentID, runtimeID, taskID).Scan(&runID)
	}
	if err != nil {
		t.Fatalf("failed to create test run: %v", err)
	}

	// Set agent status to "working"
	_, err = testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID)
	if err != nil {
		t.Fatalf("failed to set agent status: %v", err)
	}

	return taskID, agentID, runID
}

func cleanupSweeperFixture(t *testing.T, taskID, agentID string) {
	t.Helper()
	ctx := context.Background()
	testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
	testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID)
}

// ageOutAgentRuntime marks the agent's runtime as stale — old last_seen_at —
// so the runtime-liveness gate on the running-task sweep predicate
// (agent_runtime.last_seen_at within staleThresholdSeconds) does NOT protect
// the test task from being killed by the wall clock. Register a cleanup that
// restores last_seen_at so subsequent tests re-using this runtime see it as
// fresh. Callers pass a `staleAgo` well beyond staleThresholdSeconds so tests
// are insensitive to that constant's precise value.
func ageOutAgentRuntime(t *testing.T, agentID string, staleAgo time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime SET last_seen_at = now() - make_interval(secs => $1)
		WHERE id = (SELECT runtime_id FROM agent WHERE id = $2)
	`, staleAgo.Seconds(), agentID); err != nil {
		t.Fatalf("failed to age out agent runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET last_seen_at = now()
			WHERE id = (SELECT runtime_id FROM agent WHERE id = $1)
		`, agentID)
	})
}

func TestRefreshAgentStatusFromRuns(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()
	taskID, agentID, runID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })

	queries := db.New(testPool)

	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed idle agent status: %v", err)
	}

	agent, err := queries.RefreshAgentStatusFromRuns(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with dispatched task failed: %v", err)
	}
	if agent.Status != "working" {
		t.Fatalf("expected dispatched task to refresh agent status to working, got %q", agent.Status)
	}

	if _, err := testPool.Exec(ctx, `
		UPDATE agent_run
		SET status = 'cancelled', completed_at = now()
		WHERE id = $1
	`, runID); err != nil {
		t.Fatalf("failed to cancel seeded run: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to reseed working agent status: %v", err)
	}

	agent, err = queries.RefreshAgentStatusFromRuns(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("RefreshAgentStatusFromTasks with no active tasks failed: %v", err)
	}
	if agent.Status != "idle" {
		t.Fatalf("expected cancelled-only task set to refresh agent status to idle, got %q", agent.Status)
	}
}

// TestSweepStaleTasksBroadcastsWithWorkspaceID verifies that when the task sweeper
// fails a stale running task, the run:failed event is broadcast with the correct
// WorkspaceID so it reaches frontend WebSocket clients (events without WorkspaceID
// are silently dropped by the WS listener — that was the original bug).
func TestSweepStaleRunsBroadcastsWithWorkspaceID(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })
	// The running-task sweep now requires the task's runtime to be NOT
	// heartbeating (SILI-4107). Age the runtime out so this test still
	// exercises the sweeper wall clock rather than being silently skipped.
	ageOutAgentRuntime(t, agentID, 10*time.Minute)

	queries := db.New(testPool)
	bus := events.New()

	// Capture run:failed events to verify WorkspaceID is set
	var runEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("run:failed", func(e events.Event) {
		mu.Lock()
		runEvents = append(runEvents, e)
		mu.Unlock()
	})

	// Use very short timeouts to trigger the sweep on our test task
	failedRuns, err := queries.FailStaleRuns(context.Background(), db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0, // 1 second — our task is 3 hours old
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks query failed: %v", err)
	}
	if len(failedRuns) == 0 {
		t.Fatal("expected at least 1 stale task to be failed")
	}

	// Verify our task was included
	found := false
	for _, ft := range failedRuns {
		if ft.ID.Bytes == parseUUIDBytes(runID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks list", runID)
	}

	// Call broadcastFailedTasks — this is what we're testing
	broadcastFailedRuns(context.Background(), queries, nil, bus, failedRuns)

	// Verify the event was published with WorkspaceID (the core of the bug fix)
	mu.Lock()
	defer mu.Unlock()
	var foundEvent bool
	for _, e := range runEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["run_id"] == runID {
			if e.WorkspaceID == "" {
				t.Fatal("run:failed event is missing WorkspaceID — this was the original bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("expected run:failed event for task %s", runID)
	}

	// Verify DB: task should be failed
	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}
}

// TestSweepStaleTasksReconcileAgentStatus verifies that after the sweeper fails
// stale tasks, the agent status is reconciled from "working" back to "idle".
func TestSweepStaleRunsReconcileAgentStatus(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, _ := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })
	// Runtime must be stale for the running-task wall clock to fire (SILI-4107).
	ageOutAgentRuntime(t, agentID, 10*time.Minute)

	queries := db.New(testPool)
	bus := events.New()

	// Capture agent:status events
	var agentStatusEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("agent:status", func(e events.Event) {
		mu.Lock()
		agentStatusEvents = append(agentStatusEvents, e)
		mu.Unlock()
	})

	// Fail stale tasks with short timeout
	failedRuns, err := queries.FailStaleRuns(context.Background(), db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedRuns) == 0 {
		t.Fatal("expected at least 1 stale task")
	}

	broadcastFailedRuns(context.Background(), queries, nil, bus, failedRuns)

	// Verify agent status is now "idle" in DB
	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent status: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle', got '%s'", agentStatus)
	}

	// Verify agent:status event was published with correct WorkspaceID
	mu.Lock()
	defer mu.Unlock()
	if len(agentStatusEvents) == 0 {
		t.Fatal("expected agent:status event to be published")
	}
	lastEvent := agentStatusEvents[len(agentStatusEvents)-1]
	if lastEvent.WorkspaceID == "" {
		t.Fatal("agent:status event should have WorkspaceID set")
	}
	if lastEvent.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, lastEvent.WorkspaceID)
	}
}

// TestSweepDispatchedStaleTask verifies the sweeper handles dispatched tasks
// stuck beyond the dispatch timeout.
func TestSweepDispatchedStaleRun(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runID := setupSweeperTestFixture(t, "dispatched")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })

	queries := db.New(testPool)
	bus := events.New()

	// Capture run:failed events
	var runEvents []events.Event
	var mu sync.Mutex
	bus.Subscribe("run:failed", func(e events.Event) {
		mu.Lock()
		runEvents = append(runEvents, e)
		mu.Unlock()
	})

	// Fail stale tasks — dispatch timeout of 1 second (our task is 10 minutes old)
	failedRuns, err := queries.FailStaleRuns(context.Background(), db.FailStaleRunsParams{
		DispatchTimeoutSecs: 1.0,
		RunningTimeoutSecs:  9000.0,
		// RuntimeStaleSecs only affects the running branch — irrelevant for
		// this dispatched-timeout test, but wired for API consistency.
		RuntimeStaleSecs: staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}
	if len(failedRuns) == 0 {
		t.Fatal("expected at least 1 stale dispatched task")
	}

	broadcastFailedRuns(context.Background(), queries, nil, bus, failedRuns)

	// Verify DB: task should be failed
	var status string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query run: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got '%s'", status)
	}

	// Verify run:failed event was published WITH WorkspaceID
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, e := range runEvents {
		payload, _ := e.Payload.(map[string]any)
		if payload["run_id"] == runID {
			if e.WorkspaceID == "" {
				t.Fatal("run:failed event is missing WorkspaceID — this was the bug")
			}
			if e.WorkspaceID != testWorkspaceID {
				t.Fatalf("expected WorkspaceID %s, got %s", testWorkspaceID, e.WorkspaceID)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected run:failed event for task %s", runID)
	}

	// Verify agent status reconciled to idle
	var agentStatus string
	err = testPool.QueryRow(context.Background(), `SELECT status FROM agent WHERE id = $1`, agentID).Scan(&agentStatus)
	if err != nil {
		t.Fatalf("failed to query agent: %v", err)
	}
	if agentStatus != "idle" {
		t.Fatalf("expected agent status 'idle' after sweep, got '%s'", agentStatus)
	}
}

// TestSweepRunningTaskSkippedWhenRuntimeFresh is the SILI-4107 regression test:
// a running task whose wall-clock deadline has already passed MUST NOT be
// killed by the sweeper as long as its owning runtime is 'online' and its
// last_seen_at is within the runtime stale window. This preserves healthy
// multi-hour research / training runs — the primary motivation for the
// liveness-keyed sweep predicate.
func TestSweepRunningRunSkippedWhenRuntimeFresh(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })

	// Runtime heartbeat is fresh (integration fixture inserts last_seen_at=now()).
	// Task started_at is 3h ago; RunningTimeoutSecs=1s would kill on wall clock
	// alone — but the runtime is proving liveness, so the sweeper must skip it.
	queries := db.New(testPool)
	failedRuns, err := queries.FailStaleRuns(context.Background(), db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	for _, ft := range failedRuns {
		if ft.ID.Bytes == parseUUIDBytes(runID) {
			t.Fatalf("healthy long-running task on live daemon must NOT be swept — that was the SILI-4107 bug")
		}
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_run WHERE id = $1`, runID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected task to stay 'running', got %q", status)
	}
}

// TestSweepRunningTaskKilledWhenRuntimeStale is the companion coverage: with
// the same wall-clock deadline elapsed, a running task IS killed when its
// runtime's DB heartbeat is stale (simulates the "runtime lingers online
// with a stale heartbeat past the wall clock" pathological case that the
// wall-clock branch is the defensive backstop for). Note that in production
// the daemon-dead case is normally reclaimed sooner by sweepStaleRuntimes;
// this test asserts the residual backstop still fires when it needs to.
func TestSweepRunningRunKilledWhenRuntimeStale(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	taskID, agentID, runID := setupSweeperTestFixture(t, "running")
	t.Cleanup(func() { cleanupSweeperFixture(t, taskID, agentID) })
	ageOutAgentRuntime(t, agentID, 10*time.Minute)

	queries := db.New(testPool)
	failedRuns, err := queries.FailStaleRuns(context.Background(), db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	found := false
	for _, ft := range failedRuns {
		if ft.ID.Bytes == parseUUIDBytes(runID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected wall clock to fire when runtime heartbeat is stale, but task %s was not swept", runID)
	}

	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_run WHERE id = $1`, runID,
	).Scan(&status); err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected task status 'failed', got %q", status)
	}
}

// TestSweepResetsInProgressTaskToTodo verifies the core fix: when the sweeper
// force-fails a stale task whose task is still in_progress (because the daemon
// crashed mid-run), the task is reset back to todo so the daemon can re-queue it.
//
// Without this fix the task stays in_progress permanently — the agent never runs
// to update the status because it was never dispatched.
func TestSweepResetsInProgressTaskToTodo(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	// Use the same agent/runtime as the other sweeper tests.
	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create an task already in in_progress (simulates a daemon crash mid-run).
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Stuck in_progress task', 'in_progress', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Create a stale running task for the task (3 hours old — beyond any timeout).
	var runID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&runID)
	if err != nil {
		t.Fatalf("failed to create stale run: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	// Runtime must be stale for the running-task wall clock to fire (SILI-4107).
	ageOutAgentRuntime(t, agentID, 10*time.Minute)

	// Fail the stale task (running timeout of 1 second — our task is 3 hours old).
	failedRuns, err := queries.FailStaleRuns(ctx, db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	// Confirm our task was swept.
	found := false
	for _, ft := range failedRuns {
		if ft.ID.Bytes == parseUUIDBytes(runID) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected task %s to be in failed tasks, got %v", runID, failedRuns)
	}

	// This is what we're testing: task must be reset from in_progress → todo.
	broadcastFailedRuns(ctx, queries, nil, bus, failedRuns)

	var taskStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM task WHERE id = $1`, taskID).Scan(&taskStatus)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if taskStatus != "todo" {
		t.Fatalf("expected task status 'todo' after sweep, got '%s' — task is stuck", taskStatus)
	}
}

// TestSweepDoesNotResetTaskAlreadyInReview verifies that the sweeper only resets
// tasks that are truly stuck in in_progress — it must not clobber tasks whose
// agents already moved them forward (e.g. to in_review) before the task timed out.
func TestSweepDoesNotResetTaskAlreadyInReview(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Task already advanced to in_review by the agent before the task timed out.
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'Already in_review task', 'in_review', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	var runID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '3 hours', now() - interval '3 hours')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&runID)
	if err != nil {
		t.Fatalf("failed to create stale run: %v", err)
	}

	queries := db.New(testPool)
	bus := events.New()

	// Runtime must be stale for the running-task wall clock to fire (SILI-4107).
	ageOutAgentRuntime(t, agentID, 10*time.Minute)

	failedRuns, err := queries.FailStaleRuns(ctx, db.FailStaleRunsParams{
		DispatchTimeoutSecs: 300.0,
		RunningTimeoutSecs:  1.0,
		RuntimeStaleSecs:    staleThresholdSeconds,
	})
	if err != nil {
		t.Fatalf("FailStaleTasks failed: %v", err)
	}

	broadcastFailedRuns(ctx, queries, nil, bus, failedRuns)

	// Task should remain in_review — the sweeper must not clobber agent progress.
	var taskStatus string
	err = testPool.QueryRow(ctx, `SELECT status FROM task WHERE id = $1`, taskID).Scan(&taskStatus)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}
	if taskStatus != "in_review" {
		t.Fatalf("expected task status 'in_review' to be preserved, got '%s'", taskStatus)
	}
}

// TestExpireStaleQueuedTasks verifies the SILI-1899 queued-TTL sweeper:
// tasks that have been sitting in 'queued' beyond the TTL are transitioned
// to 'failed' with failure_reason='queued_expired', while fresh queued tasks
// are left alone and the per-tick batch limit is respected.
func TestExpireStaleQueuedRuns(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	// Find the integration test agent
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// One ancient queued task (should expire) and one fresh queued task (should not).
	// Constraint: idx_one_pending_run_per_task_agent → use distinct tasks.
	mkTask := func(label string) string {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET task_counter = task_counter + 1
				WHERE id = $1 RETURNING task_counter
			)
			INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, $3, 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT task_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID, label).Scan(&taskID); err != nil {
			t.Fatalf("failed to create %s task: %v", label, err)
		}
		return taskID
	}
	oldTaskID := mkTask("Queued TTL test (old)")
	freshTaskID := mkTask("Queued TTL test (fresh)")
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id IN ($1, $2)`, oldTaskID, freshTaskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id IN ($1, $2)`, oldTaskID, freshTaskID)
	})

	var oldRunID, freshRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		RETURNING id
	`, agentID, runtimeID, oldTaskID).Scan(&oldRunID); err != nil {
		t.Fatalf("failed to insert old queued run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'queued', 0, now())
		RETURNING id
	`, agentID, runtimeID, freshTaskID).Scan(&freshRunID); err != nil {
		t.Fatalf("failed to insert fresh queued run: %v", err)
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedRuns(ctx, db.ExpireStaleQueuedRunsParams{
		TtlSecs:    3600.0, // 1h TTL — old task is 5h, fresh task is 0s
		MaxPerTick: 100,
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("expected exactly 1 expired task, got %d", len(failed))
	}
	if failed[0].ID.Bytes != parseUUIDBytes(oldRunID) {
		t.Fatalf("expired the wrong run: got %x", failed[0].ID.Bytes)
	}

	// DB assertions: old → failed/queued_expired, fresh → still queued.
	var oldStatus, oldReason, oldErr string
	if err := testPool.QueryRow(ctx, `
		SELECT status, COALESCE(failure_reason, ''), COALESCE(error, '')
		FROM agent_run WHERE id = $1
	`, oldRunID).Scan(&oldStatus, &oldReason, &oldErr); err != nil {
		t.Fatalf("failed to read old run: %v", err)
	}
	if oldStatus != "failed" {
		t.Fatalf("old run: expected status=failed, got %q", oldStatus)
	}
	if oldReason != "queued_expired" {
		t.Fatalf("old run: expected failure_reason=queued_expired, got %q", oldReason)
	}
	if !strings.Contains(oldErr, "expired in queue") {
		t.Fatalf("old run: expected error to mention expiry, got %q", oldErr)
	}

	var freshStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT status FROM agent_run WHERE id = $1
	`, freshRunID).Scan(&freshStatus); err != nil {
		t.Fatalf("failed to read fresh run: %v", err)
	}
	if freshStatus != "queued" {
		t.Fatalf("fresh run: expected status=queued, got %q", freshStatus)
	}
}

// TestExpireStaleQueuedTasksRespectsBatchLimit verifies the per-tick cap so
// that a large historical backlog cannot monopolise a single sweep.
func TestExpireStaleQueuedRunsRespectsBatchLimit(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create 5 tasks, each with one stale queued task — necessary because of the
	// idx_one_pending_run_per_task_agent unique constraint.
	var taskIDs []string
	t.Cleanup(func() {
		for _, id := range taskIDs {
			testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, id)
			testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, id)
		}
	})
	for i := 0; i < 5; i++ {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			WITH bumped AS (
				UPDATE workspace SET task_counter = task_counter + 1
				WHERE id = $1 RETURNING task_counter
			)
			INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
			SELECT $1, 'Queued TTL batch test', 'todo', 'none', 'member', m.user_id, 'agent', $2, (SELECT task_counter FROM bumped)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, agentID).Scan(&taskID); err != nil {
			t.Fatalf("failed to create task %d: %v", i, err)
		}
		taskIDs = append(taskIDs, taskID)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at)
			VALUES ($1, $2, $3, 'queued', 0, now() - interval '5 hours')
		`, agentID, runtimeID, taskID); err != nil {
			t.Fatalf("failed to insert backlog task %d: %v", i, err)
		}
	}

	queries := db.New(testPool)
	failed, err := queries.ExpireStaleQueuedRuns(ctx, db.ExpireStaleQueuedRunsParams{
		TtlSecs:    3600.0,
		MaxPerTick: 2, // cap below the backlog
	})
	if err != nil {
		t.Fatalf("ExpireStaleQueuedTasks failed: %v", err)
	}
	if len(failed) != 2 {
		t.Fatalf("expected batch cap of 2, got %d", len(failed))
	}

	var remaining int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_run
		WHERE task_id = ANY($1::uuid[]) AND status = 'queued'
	`, taskIDs).Scan(&remaining); err != nil {
		t.Fatalf("failed to count remaining queued: %v", err)
	}
	if remaining != 3 {
		t.Fatalf("expected 3 queued tasks remaining after batched sweep, got %d", remaining)
	}
}

// parseUUIDBytes converts a UUID string to the 16-byte array used by pgtype.UUID.
func parseUUIDBytes(s string) [16]byte {
	s = strings.ReplaceAll(s, "-", "")
	var b [16]byte
	for i := 0; i < 16; i++ {
		hi := unhex(s[i*2])
		lo := unhex(s[i*2+1])
		b[i] = hi<<4 | lo
	}
	return b
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}
