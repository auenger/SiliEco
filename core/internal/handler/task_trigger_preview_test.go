package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seededReadyAgentID returns a workspace agent that has a runtime bound (the
// fixture's first agent), so WillEnqueueRun treats it as ready.
func seededReadyAgentID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL
		ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&id); err != nil {
		t.Fatalf("load ready agent: %v", err)
	}
	return id
}

func previewTaskTrigger(t *testing.T, body map[string]any) TaskTriggerPreviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/preview-trigger?workspace_id="+testWorkspaceID, body)
	testHandler.PreviewTaskTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewTaskTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp TaskTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return resp
}

func createTaskForTest(t *testing.T, body map[string]any) TaskResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, body)
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	t.Cleanup(func() {
		r := withURLParam(newRequest("DELETE", "/api/tasks/"+created.ID, nil), "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), r)
	})
	return created
}

func runCountFor(t *testing.T, taskID, agentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2
	`, taskID, agentID).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

// TestPreviewTaskTrigger_CreateAgentVsBacklog covers the create entry point:
// an active status with an agent assignee previews one run; the same assignee
// parked in backlog previews none.
func TestPreviewTaskTrigger_CreateAgentVsBacklog(t *testing.T) {
	agentID := seededReadyAgentID(t)

	active := previewTaskTrigger(t, map[string]any{
		"is_create":     true,
		"assignee_type": "agent",
		"assignee_id":   agentID,
		"status":        "todo",
	})
	if active.TotalCount != 1 || len(active.Triggers) != 1 {
		t.Fatalf("active create: expected 1 trigger, got %+v", active)
	}
	if active.Triggers[0].AgentID != agentID || active.Triggers[0].Source != "assign" {
		t.Fatalf("active create: wrong trigger %+v", active.Triggers[0])
	}

	backlog := previewTaskTrigger(t, map[string]any{
		"is_create":     true,
		"assignee_type": "agent",
		"assignee_id":   agentID,
		"status":        "backlog",
	})
	if backlog.TotalCount != 0 {
		t.Fatalf("backlog create: expected 0 triggers, got %+v", backlog)
	}
}

// TestPreviewTaskTrigger_MemberNoTrigger verifies a member assignee never
// previews a run.
func TestPreviewTaskTrigger_MemberNoTrigger(t *testing.T) {
	resp := previewTaskTrigger(t, map[string]any{
		"is_create":     true,
		"assignee_type": "member",
		"assignee_id":   testUserID,
		"status":        "todo",
	})
	if resp.TotalCount != 0 {
		t.Fatalf("member assignee: expected 0 triggers, got %+v", resp)
	}
}

// TestPreviewTaskTrigger_BatchAggregates verifies the batch shape: two
// agent-assigned tasks moving out of backlog preview two distinct runs.
func TestPreviewTaskTrigger_BatchAggregates(t *testing.T) {
	agentID := seededReadyAgentID(t)
	i1 := createTaskForTest(t, map[string]any{"title": "batch preview 1", "status": "backlog", "assignee_type": "agent", "assignee_id": agentID})
	i2 := createTaskForTest(t, map[string]any{"title": "batch preview 2", "status": "backlog", "assignee_type": "agent", "assignee_id": agentID})

	resp := previewTaskTrigger(t, map[string]any{
		"task_ids": []string{i1.ID, i2.ID},
		"status":   "todo",
	})
	if resp.TotalCount != 2 {
		t.Fatalf("batch promote: expected total_count 2, got %+v", resp)
	}
	seen := map[string]bool{}
	for _, tr := range resp.Triggers {
		if tr.Source != "status" {
			t.Fatalf("batch promote: expected source=status, got %q", tr.Source)
		}
		seen[tr.TaskID] = true
	}
	if !seen[i1.ID] || !seen[i2.ID] {
		t.Fatalf("batch promote: missing an task in %+v", resp.Triggers)
	}
}

// TestPreviewTaskTrigger_MatchesWritePath is the core invariant: when preview
// says a run will start, the real write path enqueues it; when preview says it
// won't, the write path enqueues nothing.
func TestPreviewTaskTrigger_MatchesWritePath(t *testing.T) {
	agentID := seededReadyAgentID(t)

	// Case 1: preview says assign will start → write path enqueues.
	task := createTaskForTest(t, map[string]any{"title": "match write 1", "status": "todo"})
	pv := previewTaskTrigger(t, map[string]any{
		"task_ids":      []string{task.ID},
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if pv.TotalCount != 1 {
		t.Fatalf("preview assign: expected 1, got %+v", pv)
	}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/tasks/"+task.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID}), "id", task.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask assign: %d %s", w.Code, w.Body.String())
	}
	if got := runCountFor(t, task.ID, agentID); got == 0 {
		t.Fatalf("preview promised a run but write path enqueued none")
	}

	// Case 2: preview says backlog assign will NOT start → write enqueues none.
	task2 := createTaskForTest(t, map[string]any{"title": "match write 2", "status": "backlog"})
	pv2 := previewTaskTrigger(t, map[string]any{
		"task_ids":      []string{task2.ID},
		"assignee_type": "agent",
		"assignee_id":   agentID,
		"status":        "backlog",
	})
	if pv2.TotalCount != 0 {
		t.Fatalf("preview backlog assign: expected 0, got %+v", pv2)
	}
	w2 := httptest.NewRecorder()
	req2 := withURLParam(newRequest("PUT", "/api/tasks/"+task2.ID, map[string]any{"assignee_type": "agent", "assignee_id": agentID}), "id", task2.ID)
	testHandler.UpdateTask(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("UpdateTask backlog assign: %d %s", w2.Code, w2.Body.String())
	}
	if got := runCountFor(t, task2.ID, agentID); got != 0 {
		t.Fatalf("preview said no run for backlog assign but write path enqueued %d", got)
	}
}

// TestUpdateTaskSuppressRunSkipsEnqueue verifies suppress_run applies the
// assignee change but starts no run, while the same write without it does.
func TestUpdateTaskSuppressRunSkipsEnqueue(t *testing.T) {
	agentID := seededReadyAgentID(t)

	// Suppressed assign: assignee set, no task.
	suppressed := createTaskForTest(t, map[string]any{"title": "suppress on", "status": "todo"})
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/tasks/"+suppressed.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID, "suppress_run": true,
	}), "id", suppressed.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask suppressed: %d %s", w.Code, w.Body.String())
	}
	if got := runCountFor(t, suppressed.ID, agentID); got != 0 {
		t.Fatalf("suppress_run=true should not enqueue, got %d tasks", got)
	}

	// Control: same write without suppress_run enqueues.
	control := createTaskForTest(t, map[string]any{"title": "suppress off", "status": "todo"})
	w2 := httptest.NewRecorder()
	req2 := withURLParam(newRequest("PUT", "/api/tasks/"+control.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID,
	}), "id", control.ID)
	testHandler.UpdateTask(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("UpdateTask control: %d %s", w2.Code, w2.Body.String())
	}
	if got := runCountFor(t, control.ID, agentID); got == 0 {
		t.Fatalf("control (no suppress_run) should enqueue, got 0 tasks")
	}
}

// TestUpdateTaskHandoffNotePersistsOnRun verifies an assign carrying a
// handoff_note writes that note onto the enqueued task (the daemon then renders
// it), while a suppressed assign with a note enqueues nothing at all.
func TestUpdateTaskHandoffNotePersistsOnRun(t *testing.T) {
	agentID := seededReadyAgentID(t)
	note := "Only touch the login flow."

	task := createTaskForTest(t, map[string]any{"title": "handoff persist", "status": "todo"})
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/tasks/"+task.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID, "handoff_note": note,
	}), "id", task.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask with handoff: %d %s", w.Code, w.Body.String())
	}

	var stored string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(handoff_note, '') FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 ORDER BY created_at DESC LIMIT 1
	`, task.ID, agentID).Scan(&stored); err != nil {
		t.Fatalf("read task handoff_note: %v", err)
	}
	if stored != note {
		t.Fatalf("expected task handoff_note %q, got %q", note, stored)
	}

	// Suppressed assign with a note: no task at all (no run to inject into).
	suppressed := createTaskForTest(t, map[string]any{"title": "handoff suppressed", "status": "todo"})
	w2 := httptest.NewRecorder()
	req2 := withURLParam(newRequest("PUT", "/api/tasks/"+suppressed.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID, "handoff_note": note, "suppress_run": true,
	}), "id", suppressed.ID)
	testHandler.UpdateTask(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("UpdateTask suppressed handoff: %d %s", w2.Code, w2.Body.String())
	}
	if got := runCountFor(t, suppressed.ID, agentID); got != 0 {
		t.Fatalf("suppressed handoff should enqueue no task, got %d", got)
	}
}

// TestPreviewTaskTrigger_MalformedBody verifies the endpoint rejects a
// malformed body with 400 rather than a 500 or a silent empty result.
func TestPreviewTaskTrigger_MalformedBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tasks/preview-trigger?workspace_id="+testWorkspaceID, strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.PreviewTaskTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
