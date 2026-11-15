package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertRunningTaskRun inserts an in-flight (running) task for the given agent
// on the given task and returns its id, registering cleanup.
func insertRunningTaskRun(t *testing.T, agentID, taskID string) string {
	t.Helper()
	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, started_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, now())
		RETURNING id
	`, agentID, taskID).Scan(&runID); err != nil {
		t.Fatalf("insert running run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })
	return runID
}

// insertAgentAssignedTask inserts an task assigned to the given agent and
// returns its id, registering cleanup.
func insertAgentAssignedTask(t *testing.T, agentID string, number int, title string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, $2, 'todo', 'medium', $3, 'member', $4, 0, 'agent', $5)
		RETURNING id
	`, testWorkspaceID, title, testUserID, number, agentID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID) })
	return taskID
}

// TestUpdateTaskReassignDoesNotCancelActiveRuns locks in the #4963 / SILI-4113
// decision: changing an task's assignee cancels nothing. Both the previous
// assignee's own in-flight run and an unrelated (mention-triggered) run for a
// different agent must survive the reassignment.
func TestUpdateTaskReassignDoesNotCancelActiveRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "ReassignNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "ReassignNoCancelMention", []byte("[]"))

	taskID := insertAgentAssignedTask(t, ownerAgent, 92110, "reassign-no-cancel")
	ownerRun := insertRunningTaskRun(t, ownerAgent, taskID)
	mentionRun := insertRunningTaskRun(t, mentionAgent, taskID)

	// Reassign from ownerAgent to a member — a genuine assignee change that
	// does not itself enqueue a new agent run.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID, map[string]any{
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	req = withURLParam(req, "id", taskID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := runStatus(t, ownerRun); got != "running" {
		t.Fatalf("previous assignee's own task must survive reassignment, got status %q", got)
	}
	if got := runStatus(t, mentionRun); got != "running" {
		t.Fatalf("unrelated agent's task must survive reassignment (no collateral cancel), got status %q", got)
	}
}

// TestBatchUpdateTaskReassignDoesNotCancelActiveRuns is the batch-path mirror
// of TestUpdateTaskReassignDoesNotCancelActiveRuns — BatchUpdateTasks shares
// the same no-cancel-on-reassign behavior.
func TestBatchUpdateTaskReassignDoesNotCancelActiveRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "BatchReassignNoCancelOwner", []byte("[]"))
	mentionAgent := createHandlerTestAgent(t, "BatchReassignNoCancelMention", []byte("[]"))

	taskID := insertAgentAssignedTask(t, ownerAgent, 92111, "batch-reassign-no-cancel")
	ownerRun := insertRunningTaskRun(t, ownerAgent, taskID)
	mentionRun := insertRunningTaskRun(t, mentionAgent, taskID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": []string{taskID},
		"updates": map[string]any{
			"assignee_type": "member",
			"assignee_id":   testUserID,
		},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateTasks reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := runStatus(t, ownerRun); got != "running" {
		t.Fatalf("previous assignee's own task must survive batch reassignment, got status %q", got)
	}
	if got := runStatus(t, mentionRun); got != "running" {
		t.Fatalf("unrelated agent's task must survive batch reassignment, got status %q", got)
	}
}

// queuedTaskCountFor returns how many queued tasks the agent holds on the task.
func queuedRunCountFor(t *testing.T, taskID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

// TestUpdateTaskReassignToAgentKeepsOldRunAndEnqueuesNew covers the core
// handoff path the member-target tests above do not: reassigning from one agent
// to ANOTHER agent. The previous assignee's in-flight run must survive (the
// #4963 / SILI-4113 no-cancel guarantee), and the new assignee must still get
// its run enqueued by WillEnqueueRun — the two effects are independent.
func TestUpdateTaskReassignToAgentKeepsOldRunAndEnqueuesNew(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerAgent := createHandlerTestAgent(t, "ReassignAgentToAgentOwner", []byte("[]"))
	newAgent := createHandlerTestAgent(t, "ReassignAgentToAgentNew", []byte("[]"))

	taskID := insertAgentAssignedTask(t, ownerAgent, 92112, "reassign-agent-to-agent")
	ownerRun := insertRunningTaskRun(t, ownerAgent, taskID)

	// Reassign from ownerAgent to newAgent — an agent→agent ownership handoff.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   newAgent,
	})
	req = withURLParam(req, "id", taskID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask reassign: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := runStatus(t, ownerRun); got != "running" {
		t.Fatalf("previous agent's own task must survive agent→agent reassignment, got status %q", got)
	}
	if got := queuedRunCountFor(t, taskID, newAgent); got != 1 {
		t.Fatalf("new assignee must get exactly one run enqueued, got %d queued tasks", got)
	}
}
