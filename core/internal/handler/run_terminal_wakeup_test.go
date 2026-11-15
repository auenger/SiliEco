package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type terminalWakeupRecorder struct {
	calls []struct{ runtimeID, runID string }
}

func (r *terminalWakeupRecorder) NotifyRunAvailable(runtimeID, runID string) {
	r.calls = append(r.calls, struct{ runtimeID, runID string }{runtimeID, runID})
}

func failRunViaHandler(t *testing.T, runID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runs/"+runID+"/fail",
		map[string]any{"error": "agent failed", "failure_reason": "agent_error"},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runId", runID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.FailRun(w, req)
	return w
}

// TestTerminalTransitionsNotifyRuntime verifies the server-side half of the
// queued-successor fix. Completion wakes only after reconciliation, while fail
// and cancel wake after their terminal side effects, so an old daemon can retry
// its normal atomic claim path without waiting for the 30-second fallback.
func TestTerminalTransitionsNotifyRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'terminal wakeup fixture', 'in_progress', 'none', $2, 'member', 999099, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID) })

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&runID); err != nil {
		t.Fatalf("setup: running run: %v", err)
	}

	recorder := &terminalWakeupRecorder{}
	previous := testHandler.AgentRunService.Wakeup
	testHandler.AgentRunService.Wakeup = recorder
	t.Cleanup(func() { testHandler.AgentRunService.Wakeup = previous })

	if w := completeRunViaHandler(t, runID, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(recorder.calls); got != 1 {
		t.Fatalf("expected 1 terminal runtime wakeup, got %d", got)
	}
	if recorder.calls[0].runtimeID != runtimeID {
		t.Fatalf("wakeup runtime = %q, want %q", recorder.calls[0].runtimeID, runtimeID)
	}
	if recorder.calls[0].runID != "" {
		t.Fatalf("terminal wakeup must omit completed task id, got %q", recorder.calls[0].runID)
	}

	// Failure uses the daemon callback handler, matching production. It must
	// send the same runtime-only hint after FailTask commits.
	recorder.calls = nil
	var failedRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&failedRunID); err != nil {
		t.Fatalf("setup: failed run: %v", err)
	}
	if w := failRunViaHandler(t, failedRunID); w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := len(recorder.calls); got != 1 || recorder.calls[0].runtimeID != runtimeID || recorder.calls[0].runID != "" {
		t.Fatalf("unexpected failure wakeups: %#v", recorder.calls)
	}

	// Cancellation is finalized in TaskService because both task and chat
	// handlers share that path.
	recorder.calls = nil
	var cancelledRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, created_at, started_at)
		VALUES ($1, $2, $3, 'running', 0, now() - interval '2 minutes', now() - interval '1 minute')
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&cancelledRunID); err != nil {
		t.Fatalf("setup: cancelled run: %v", err)
	}
	cancelled, err := testHandler.AgentRunService.CancelRun(ctx, parseUUID(cancelledRunID))
	if err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Fatalf("CancelTask status = %q, want cancelled", cancelled.Status)
	}
	if got := len(recorder.calls); got != 1 || recorder.calls[0].runtimeID != runtimeID || recorder.calls[0].runID != "" {
		t.Fatalf("unexpected cancellation wakeups: %#v", recorder.calls)
	}
}
