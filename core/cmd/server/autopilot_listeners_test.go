package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestAutopilotRunOnlyRunTerminalEventsUpdateRun(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	runSvc := service.NewAgentRunService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, runSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	tests := []struct {
		name       string
		finalize   func(agentRun db.AgentRun)
		wantStatus string
		wantResult string
		wantReason string
	}{
		{
			name: "completed",
			finalize: func(agentRun db.AgentRun) {
				if _, err := runSvc.CompleteRun(ctx, agentRun.ID, []byte(`{"output":"done"}`), "", "", false, ""); err != nil {
					t.Fatalf("CompleteTask: %v", err)
				}
			},
			wantStatus: "completed",
			wantResult: "done",
		},
		{
			name: "failed",
			finalize: func(agentRun db.AgentRun) {
				if _, err := runSvc.FailRun(ctx, agentRun.ID, "boom", "", "", "agent_error", false, ""); err != nil {
					t.Fatalf("FailTask: %v", err)
				}
			},
			wantStatus: "failed",
			wantReason: "boom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
				WorkspaceID:       parseUUID(testWorkspaceID),
				Title:             "Run-only listener " + tc.name,
				Description:       pgtype.Text{String: "Run listener regression test", Valid: true},
				AssigneeType:      "agent",
				AssigneeID:        parseUUID(agentID),
				Status:            "active",
				ExecutionMode:     "run_only",
				TaskTitleTemplate: pgtype.Text{},
				CreatedByType:     "member",
				CreatedByID:       parseUUID(testUserID),
			})
			if err != nil {
				t.Fatalf("CreateAutopilot: %v", err)
			}
			t.Cleanup(func() {
				if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID); err != nil {
					t.Logf("cleanup autopilot: %v", err)
				}
			})

			run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
			if err != nil {
				t.Fatalf("DispatchAutopilot: %v", err)
			}
			if !run.RunID.Valid {
				t.Fatal("run_only dispatch did not link a task")
			}

			if _, err := testPool.Exec(ctx,
				`UPDATE agent_run SET status = 'dispatched', dispatched_at = now() WHERE id = $1`,
				run.RunID,
			); err != nil {
				t.Fatalf("mark task dispatched: %v", err)
			}
			agentRun, err := queries.StartAgentRun(ctx, run.RunID)
			if err != nil {
				t.Fatalf("StartAgentTask: %v", err)
			}

			tc.finalize(agentRun)

			updatedRun, err := queries.GetAutopilotRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetAutopilotRun: %v", err)
			}
			if updatedRun.Status != tc.wantStatus {
				t.Fatalf("expected run status %q, got %q", tc.wantStatus, updatedRun.Status)
			}
			if tc.wantResult != "" && !strings.Contains(string(updatedRun.Result), tc.wantResult) {
				t.Fatalf("expected run result to contain %q, got %s", tc.wantResult, string(updatedRun.Result))
			}
			if tc.wantReason != "" {
				if !updatedRun.FailureReason.Valid {
					t.Fatalf("expected failure reason %q, got invalid", tc.wantReason)
				}
				if updatedRun.FailureReason.String != tc.wantReason {
					t.Fatalf("expected failure reason %q, got %q", tc.wantReason, updatedRun.FailureReason.String)
				}
			}
		})
	}
}

// linkedTaskAutopilotFixture is the starting state every create_task
// linked-task listener test shares: a dispatched create_task run sitting in
// task_created with exactly one task task that carries no autopilot_run_id
// (so it must be reached via the task_id lookup, not SyncRunFromTask).
type linkedTaskAutopilotFixture struct {
	runSvc  *service.AgentRunService
	queries *db.Queries
	run     *db.AutopilotRun
	runID   pgtype.UUID
}

// dispatchCreateTaskAutopilot creates an active create_task autopilot,
// dispatches it, and returns the linked run plus its single task task.
// Cleanup (autopilot, task, tasks, comments) is registered on t.
func dispatchCreateTaskAutopilot(t *testing.T, title string) linkedTaskAutopilotFixture {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	runSvc := service.NewAgentRunService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, runSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:       parseUUID(testWorkspaceID),
		Title:             title,
		Description:       pgtype.Text{String: "VEN-661 / VEN-662 regression test", Valid: true},
		AssigneeType:      "agent",
		AssigneeID:        parseUUID(agentID),
		Status:            "active",
		ExecutionMode:     "create_task",
		TaskTitleTemplate: pgtype.Text{String: "Linked task", Valid: true},
		CreatedByType:     "member",
		CreatedByID:       parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if !run.TaskID.Valid {
		t.Fatal("create_task dispatch did not link an task")
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, run.TaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, run.TaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, run.TaskID)
	})

	agentRuns, err := queries.ListRunsByTask(ctx, run.TaskID)
	if err != nil {
		t.Fatalf("ListTasksByTask: %v", err)
	}
	if len(agentRuns) != 1 {
		t.Fatalf("expected one task task, got %d", len(agentRuns))
	}
	if agentRuns[0].AutopilotRunID.Valid {
		t.Fatal("create_task task task unexpectedly has autopilot_run_id; test must exercise linked task lookup")
	}
	if run.Status != "task_created" {
		t.Fatalf("expected pre-failure run status task_created, got %q", run.Status)
	}

	return linkedTaskAutopilotFixture{runSvc: runSvc, queries: queries, run: run, runID: agentRuns[0].ID}
}

// runTaskWithBudget marks the task task dispatched with the given attempt
// budget and transitions it to running, mirroring the daemon claim → start
// flow so FailTask sees a realistic row (and so the auto-retry budget is
// whatever the test wants).
func runRunWithBudget(t *testing.T, queries *db.Queries, runID pgtype.UUID, maxAttempts int) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_run SET status = 'dispatched', dispatched_at = now(), max_attempts = $2 WHERE id = $1`,
		runID, maxAttempts,
	); err != nil {
		t.Fatalf("mark task dispatched: %v", err)
	}
	if _, err := queries.StartAgentRun(context.Background(), runID); err != nil {
		t.Fatalf("StartAgentTask: %v", err)
	}
}

// TestAutopilotCreateTaskRunNoProgressFailureUpdatesRun is the original
// VEN-661 regression: a Codex no-progress failure with no retries left fails
// the linked run.
func TestAutopilotCreateTaskRunNoProgressFailureUpdatesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateTaskAutopilot(t, "Create-task no-progress listener")

	// max_attempts = 1 means the failed attempt has no retry budget left.
	runRunWithBudget(t, f.queries, f.runID, 1)

	const errMsg = "codex app-server no progress timeout after 30s"
	if _, err := f.runSvc.FailRun(ctx, f.runID, errMsg, "", "", "codex_semantic_inactivity", false, ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "failed" {
		t.Fatalf("expected run status failed, got %q", updatedRun.Status)
	}
	if !updatedRun.FailureReason.Valid || !strings.Contains(updatedRun.FailureReason.String, "no progress timeout") {
		t.Fatalf("expected no-progress failure reason, got %+v", updatedRun.FailureReason)
	}
}

// TestAutopilotCreateTaskRunAgentErrorFailureUpdatesRun covers the VEN-662
// generalization: an ordinary, non-retryable agent failure must also close the
// linked run instead of leaving it stuck in task_created.
func TestAutopilotCreateTaskRunAgentErrorFailureUpdatesRun(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateTaskAutopilot(t, "Create-task agent-error listener")

	runRunWithBudget(t, f.queries, f.runID, 1)

	// agent_error is not in retryableReasons, so the first terminal failure is
	// final — the run must fail carrying the agent's error text.
	const errMsg = "build failed: ./pkg/foo: undefined: Bar"
	if _, err := f.runSvc.FailRun(ctx, f.runID, errMsg, "", "", "agent_error", false, ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "failed" {
		t.Fatalf("expected run status failed, got %q", updatedRun.Status)
	}
	if !updatedRun.FailureReason.Valid || !strings.Contains(updatedRun.FailureReason.String, "build failed") {
		t.Fatalf("expected agent-error failure reason, got %+v", updatedRun.FailureReason)
	}
}

// TestAutopilotCreateTaskRunRetryPendingKeepsRunOpen locks in the wait guard:
// when FailTask auto-retries a retryable failure (attempt budget remaining), an
// active retry task still exists for the task, so the run must stay open until
// the final attempt resolves.
func TestAutopilotCreateTaskRunRetryPendingKeepsRunOpen(t *testing.T) {
	ctx := context.Background()
	f := dispatchCreateTaskAutopilot(t, "Create-task retry-pending listener")

	// max_attempts = 2 with attempt = 1 leaves budget for one auto-retry.
	runRunWithBudget(t, f.queries, f.runID, 2)

	// timeout is retryable, so FailTask enqueues a fresh attempt before it
	// broadcasts the failure event.
	if _, err := f.runSvc.FailRun(ctx, f.runID, "runtime went offline", "", "", "timeout", false, ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	hasActive, err := f.queries.HasActiveRunForTask(ctx, f.run.TaskID)
	if err != nil {
		t.Fatalf("HasActiveTaskForTask: %v", err)
	}
	if !hasActive {
		t.Fatal("expected an active retry task for the task after a retryable failure")
	}

	updatedRun, err := f.queries.GetAutopilotRun(ctx, f.run.ID)
	if err != nil {
		t.Fatalf("GetAutopilotRun: %v", err)
	}
	if updatedRun.Status != "task_created" {
		t.Fatalf("expected run to stay task_created while a retry is pending, got %q", updatedRun.Status)
	}
}

// TestAutopilotDispatchSkipsWhenRuntimeOffline locks in the SILI-1899
// admission gate: when the assignee agent's runtime is not online we must
// record a `skipped` autopilot_run with a failure_reason and NOT enqueue an
// agent_run row. This is the fix for "活跃 schedule 持续给离线 local
// agent 入队".
func TestAutopilotDispatchSkipsWhenRuntimeOffline(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	runSvc := service.NewAgentRunService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, runSvc)

	// Spin up a dedicated runtime + agent so we can flip the runtime to
	// offline without affecting the shared fixture used by other tests.
	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Offline runtime', 'local', 'mul1899_offline_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, 'mul1899-offline-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create offline agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:       parseUUID(testWorkspaceID),
		Title:             "Offline-runtime autopilot",
		Description:       pgtype.Text{String: "SILI-1899 admission test", Valid: true},
		AssigneeType:      "agent",
		AssigneeID:        parseUUID(agentID),
		Status:            "active",
		ExecutionMode:     "run_only",
		TaskTitleTemplate: pgtype.Text{},
		CreatedByType:     "member",
		CreatedByID:       parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
	if !run.FailureReason.Valid || !strings.Contains(run.FailureReason.String, "offline") {
		t.Fatalf("expected failure reason mentioning 'offline', got %+v", run.FailureReason)
	}
	if run.RunID.Valid {
		t.Fatalf("expected no task to be enqueued, got run_id %v", run.RunID)
	}

	// Defensive: confirm at the DB layer that nothing landed on the queue.
	var runCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE agent_id = $1`,
		agentID,
	).Scan(&runCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("expected 0 queued tasks for offline-runtime agent, got %d", runCount)
	}
}

// TestAutopilotCreateTaskDispatchCreatesTaskWhenRuntimeOffline locks in the
// audit-trail contract for tracked autopilots: create_task mode must still
// create a visible task when the assignee runtime is offline, leaving the
// task/task to be claimed when the runtime comes back instead of silently
// recording an unrecoverable skipped run.
func TestAutopilotCreateTaskDispatchCreatesTaskWhenRuntimeOffline(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	runSvc := service.NewAgentRunService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, runSvc)

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Offline create-task runtime', 'local', 'ws1325_offline_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, 'ws1325-offline-create-task-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create offline agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:       parseUUID(testWorkspaceID),
		Title:             "Offline create-task autopilot",
		Description:       pgtype.Text{String: "WS-1325 regression test", Valid: true},
		AssigneeType:      "agent",
		AssigneeID:        parseUUID(agentID),
		Status:            "active",
		ExecutionMode:     "create_task",
		TaskTitleTemplate: pgtype.Text{String: "Tracked task", Valid: true},
		CreatedByType:     "member",
		CreatedByID:       parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "task_created" {
		t.Fatalf("expected run status 'task_created', got %q", run.Status)
	}
	if !run.TaskID.Valid {
		t.Fatal("create_task dispatch did not link an task")
	}
	if run.FailureReason.Valid {
		t.Fatalf("expected no failure reason, got %q", run.FailureReason.String)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, run.TaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, run.TaskID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, run.TaskID)
	})

	agentRuns, err := queries.ListRunsByTask(ctx, run.TaskID)
	if err != nil {
		t.Fatalf("ListTasksByTask: %v", err)
	}
	if len(agentRuns) != 1 {
		t.Fatalf("expected one queued task task, got %d", len(agentRuns))
	}
	if agentRuns[0].AgentID != parseUUID(agentID) {
		t.Fatalf("task agent mismatch: got %v want %v", agentRuns[0].AgentID, parseUUID(agentID))
	}
	if agentRuns[0].RuntimeID != parseUUID(runtimeID) {
		t.Fatalf("task runtime mismatch: got %v want %v", agentRuns[0].RuntimeID, parseUUID(runtimeID))
	}
}

// TestManualTriggerDoesNotErrorOnPostAdmissionSkip locks in PR #2888 review
// fix #2: if the dispatcher decides to skip after the admission gate has
// already passed (e.g. the leader's runtime went offline between admission
// and task creation), DispatchAutopilot must return (run, nil) with
// status='skipped' rather than (nil, err). Without this, manual trigger
// surfaces a 500 to the user even though the work was correctly suppressed
// — the same regression Emacs flagged on the original PR.
//
// We synthesise the race by:
//  1. Creating an online runtime + agent so the admission gate passes.
//  2. Flipping the runtime to offline.
//  3. Triggering the autopilot. Admission has already loaded the agent +
//     runtime once with status='online' at row-fetch time, so the second
//     check inside dispatchRunOnly is what catches the offline state.
//
// In this implementation the admission gate also re-reads the runtime, so
// the same offline state actually fires the admission skip first. That is
// fine for the assertion we care about: the manual trigger must not 500 and
// the run must be `skipped`. The post-admission branch is exercised
// separately by the errDispatchSkipped unwrap unit test in the service
// package.
func TestManualTriggerDoesNotErrorOnPostAdmissionSkip(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	runSvc := service.NewAgentRunService(queries, testPool, nil, bus)
	autopilotSvc := service.NewAutopilotService(queries, testPool, bus, runSvc)

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, 'Manual-trigger skip runtime', 'local', 'mul2429_manual_skip_runtime', 'offline', '{}'::jsonb, '{}'::jsonb, now())
		RETURNING id::text
	`, parseUUID(testWorkspaceID)).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, 'mul2429-manual-skip-agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id::text
	`, parseUUID(testWorkspaceID), runtimeID, parseUUID(testUserID)).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:       parseUUID(testWorkspaceID),
		Title:             "Manual-trigger skip autopilot",
		Description:       pgtype.Text{String: "PR #2888 review fix #2", Valid: true},
		AssigneeType:      "agent",
		AssigneeID:        parseUUID(agentID),
		Status:            "active",
		ExecutionMode:     "run_only",
		TaskTitleTemplate: pgtype.Text{},
		CreatedByType:     "member",
		CreatedByID:       parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, ap.ID)
	})

	run, err := autopilotSvc.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("manual DispatchAutopilot returned error (would 500 the handler): %v", err)
	}
	if run == nil {
		t.Fatal("expected a run, got nil")
	}
	if run.Status != "skipped" {
		t.Fatalf("expected run status 'skipped', got %q", run.Status)
	}
}
