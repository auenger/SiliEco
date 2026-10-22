package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteUnusedWorkflow(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	var projectID string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO project (workspace_id, title) VALUES ($1, 'SOP deletion test') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, projectID) })
	cleanupWorkflowTestData(t)
	create := func() WorkflowResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.CreateWorkflow(w, newRequest(http.MethodPost, "/api/workflows", map[string]any{"name": "Deletion test", "project_id": projectID, "publish": true, "stages": []map[string]any{{"name": "Plan"}}}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
		var workflow WorkflowResponse
		if err := json.Unmarshal(w.Body.Bytes(), &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow
	}
	remove := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.ArchiveWorkflow(w, workflowRequest(http.MethodDelete, "/api/workflows/"+id+"?unused_only=true", nil, map[string]string{"id": id}))
		return w
	}
	unused := create()
	deleted := remove(unused.ID)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete unused: %d %s", deleted.Code, deleted.Body.String())
	}
	var result WorkflowResponse
	if err := json.Unmarshal(deleted.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "archived" {
		t.Fatalf("unexpected status: %s", result.Status)
	}
	used := create()
	run := httptest.NewRecorder()
	testHandler.CreateWorkflowInstance(run, workflowRequest(http.MethodPost, "/api/workflows/"+used.ID+"/instances", map[string]any{"title": "Not started", "start": false}, map[string]string{"id": used.ID}))
	if run.Code != http.StatusCreated {
		t.Fatalf("create run: %d %s", run.Code, run.Body.String())
	}
	blocked := remove(used.ID)
	if blocked.Code != http.StatusConflict {
		t.Fatalf("expected conflict: %d %s", blocked.Code, blocked.Body.String())
	}
}
