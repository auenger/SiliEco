package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTaskInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "invalid status task",
		"status": "active",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "backlog") {
		t.Errorf("expected error to list valid statuses, got: %s", body)
	}
}

func TestCreateTaskInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "invalid priority task",
		"priority": "P1",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "urgent") {
		t.Errorf("expected error to list valid priorities, got: %s", body)
	}
}

func TestUpdateTaskInvalidStatusReturns400(t *testing.T) {
	taskID := createTestTask(t, "update invalid status task", "todo", "none")
	t.Cleanup(func() { deleteTestTask(t, taskID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID, map[string]any{"status": "active"})
	req = withURLParam(req, "id", taskID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateTaskInvalidPriorityReturns400(t *testing.T) {
	taskID := createTestTask(t, "update invalid priority task", "todo", "none")
	t.Cleanup(func() { deleteTestTask(t, taskID) })

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID, map[string]any{"priority": "P1"})
	req = withURLParam(req, "id", taskID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchUpdateTasksInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": []string{"not-needed"},
		"updates": map[string]any{
			"status": "active",
		},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchUpdateTasksInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": []string{"not-needed"},
		"updates": map[string]any{
			"priority": "P1",
		},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}
