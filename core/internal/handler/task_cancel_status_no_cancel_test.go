package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// activeTaskStatuses are the non-terminal states CancelAgentRunsByTask sweeps
// (core/pkg/db/queries/agent.sql). SILI-4465's contract is that a status flip
// to `cancelled` leaves every one of them untouched, so the tests below drive
// each state, not just `running`.
var activeRunStatuses = []string{"queued", "dispatched", "running", "waiting_local_directory", "deferred"}

// insertTaskRunWithStatus inserts one active task for the agent on the task
// in the given status, populating the per-status columns the schema expects
// (started_at for running, fire_at for deferred, wait_reason for
// waiting_local_directory), and registers cleanup.
func insertTaskRunWithStatus(t *testing.T, agentID, taskID, status string) string {
	t.Helper()
	var startedAt, fireAt, waitReason any // nil => SQL NULL
	switch status {
	case "running":
		startedAt = time.Now()
	case "deferred":
		fireAt = time.Now().Add(time.Hour)
	case "waiting_local_directory":
		waitReason = "waiting for local directory"
	}
	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, started_at, fire_at, wait_reason)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 0, $3, $4, $5, $6)
		RETURNING id
	`, agentID, status, taskID, startedAt, fireAt, waitReason).Scan(&runID); err != nil {
		t.Fatalf("insert %s run: %v", status, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })
	return runID
}

// TestUpdateTaskCancelStatusDoesNotCancelActiveRuns locks in SILI-4465:
// moving an task to `cancelled` no longer stops its in-flight agent runs. A
// user clicking "cancel" has no expectation that it interrupts running tasks,
// so that implicit coupling was removed. Every active task state must survive,
// as must an unrelated @-mention run for a different agent on the same task.
// Deleting an task still cancels its tasks (covered elsewhere).
func TestUpdateTaskCancelStatusDoesNotCancelActiveRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "CancelStatusNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "CancelStatusNoCancelMention", []byte("[]"))

	for i, status := range activeRunStatuses {
		t.Run(status, func(t *testing.T) {
			taskID := insertAgentAssignedTask(t, ownerAgent, 92130+i, "cancel-status-no-cancel-"+status)
			ownerRun := insertTaskRunWithStatus(t, ownerAgent, taskID, status)
			mentionRun := insertRunningTaskRun(t, mentionAgent, taskID)

			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/tasks/"+taskID, map[string]any{
				"status": "cancelled",
			})
			req = withURLParam(req, "id", taskID)
			testHandler.UpdateTask(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("UpdateTask cancel: expected 200, got %d: %s", w.Code, w.Body.String())
			}

			if got := runStatus(t, ownerRun); got != status {
				t.Fatalf("assignee's %s task must survive task → cancelled, got status %q", status, got)
			}
			if got := runStatus(t, mentionRun); got != "running" {
				t.Fatalf("unrelated agent's task must survive task → cancelled, got status %q", got)
			}
		})
	}
}

// TestBatchUpdateTaskCancelStatusDoesNotCancelActiveRuns is the batch-path
// mirror — BatchUpdateTasks shares the same no-cancel-on-cancelled behavior.
// One task per active state is cancelled in a single batch call; every task
// must keep its original status.
func TestBatchUpdateTaskCancelStatusDoesNotCancelActiveRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "BatchCancelStatusNoCancelOwner", []byte("[]"))

	taskIDs := make([]string, 0, len(activeRunStatuses))
	runByStatus := make(map[string]string, len(activeRunStatuses))
	for i, status := range activeRunStatuses {
		taskID := insertAgentAssignedTask(t, ownerAgent, 92140+i, "batch-cancel-status-no-cancel-"+status)
		runByStatus[status] = insertTaskRunWithStatus(t, ownerAgent, taskID, status)
		taskIDs = append(taskIDs, taskID)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": taskIDs,
		"updates": map[string]any{
			"status": "cancelled",
		},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateTasks cancel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for status, runID := range runByStatus {
		if got := runStatus(t, runID); got != status {
			t.Fatalf("%s task must survive batch task → cancelled, got status %q", status, got)
		}
	}
}
