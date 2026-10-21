package handler

import (
	"context"
	"encoding/json"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testWorkflowReworkAPI(t *testing.T, runID string) {
	t.Helper()
	ctx := context.Background()
	var sourceID, stageID string
	if err := testPool.QueryRow(ctx, `SELECT id,workflow_stage_id FROM workflow_stage_execution WHERE workflow_instance_id=$1 ORDER BY attempt DESC LIMIT 1`, runID).Scan(&sourceID, &stageID); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": runID, "executionId": sourceID}
	post := func(body any, agent bool) *httptest.ResponseRecorder {
		r := workflowRequest(http.MethodPost, "/rework", body, params)
		if agent {
			r.Header.Set("X-Task-ID", sourceID)
		}
		w := httptest.NewRecorder()
		testHandler.ReworkWorkflowExecution(w, r)
		return w
	}
	body := map[string]any{"expected_revision": 1, "target_stage_id": stageID, "reason": "Revise delivery"}
	if w := post(body, true); w.Code != http.StatusForbidden {
		t.Fatalf("agent initiated rework: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []any{
		map[string]any{"expected_revision": 0, "target_stage_id": stageID, "reason": "fix"},
		map[string]any{"expected_revision": 1, "target_stage_id": "invalid", "reason": "fix"},
		map[string]any{"expected_revision": 1, "target_stage_id": stageID, "reason": " "},
		map[string]any{"expected_revision": 1, "target_stage_id": stageID, "reason": "fix", "actor_id": testUserID},
	} {
		if w := post(bad, false); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid rework: %d %s", w.Code, w.Body.String())
		}
	}
	if w := post(map[string]any{"expected_revision": 99, "target_stage_id": stageID, "reason": "fix"}, false); w.Code != http.StatusConflict {
		t.Fatalf("stale revision reworked: %d %s", w.Code, w.Body.String())
	}
	first := post(body, false)
	var response struct {
		ExecutionID string `json:"execution_id"`
		SourceID    string `json:"source_execution_id"`
		InstanceID  string `json:"instance_id"`
		StageID     string `json:"stage_id"`
		Attempt     int    `json:"attempt"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil || first.Code != http.StatusOK || response.ExecutionID == sourceID || response.SourceID != sourceID || response.InstanceID != runID || response.StageID != stageID || response.Attempt != 2 || response.Status != "pending" {
		t.Fatalf("rework: %d %s %v", first.Code, first.Body.String(), err)
	}
	if w := post(body, false); w.Code != http.StatusOK || w.Body.String() != first.Body.String() {
		t.Fatalf("duplicate rework: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, runID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate business attempts: %d %v", count, err)
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE workflow_instance_id=$1 AND status='pending'`, runID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("old dispatch survived: %d %v", count, err)
	}
	testWorkflowReworkToManualAPI(t, runID, response.ExecutionID, stageID)
}

func TestWorkflowInvalidationResponsesPreserveHistoryMarker(t *testing.T) {
	source := parseUUID("11111111-1111-4111-8111-111111111111")
	issue := issueToResponse(db.Issue{WorkflowInvalidatedByReworkID: source}, "T")
	if issue.WorkflowInvalidatedByReworkID == nil || *issue.WorkflowInvalidatedByReworkID != uuidToString(source) {
		t.Fatal("issue invalidation marker omitted")
	}
	decision := workflowDecisionToResponse(db.WorkflowGateDecision{InvalidatedByReworkID: source, Outcome: "approved"})
	if decision.InvalidatedByReworkID == nil || *decision.InvalidatedByReworkID != uuidToString(source) || decision.Outcome != "approved" {
		t.Fatal("historical decision lost its outcome or invalidation marker")
	}
}

func TestWorkflowInvalidationSurvivesIssueListAndSearch(t *testing.T) {
	create := httptest.NewRecorder()
	testHandler.CreateIssue(create, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "rework-marker-visibility", "status": "todo", "priority": "none"}))
	if create.Code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", create.Code, create.Body.String())
	}
	var created IssueResponse
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1 AND workspace_id=$2`, created.ID, testWorkspaceID)
	})
	const source = "11111111-1111-4111-8111-111111111111"
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET workflow_instance_id=$2,workflow_stage_id=$2,workflow_invalidated_by_rework_id=$2 WHERE id=$1`, created.ID, source); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		name, path string
		handler    http.HandlerFunc
	}{
		{"list", "/api/issues?q=rework-marker-visibility", testHandler.ListIssues},
		{"grouped", "/api/issues/grouped?group_by=assignee&limit=100", testHandler.ListGroupedIssues},
		{"search", "/api/issues/search?q=rework-marker-visibility", testHandler.SearchIssues},
	} {
		t.Run(route.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			route.handler(response, newRequest(http.MethodGet, route.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("response: %d %s", response.Code, response.Body.String())
			}
			var body any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			found := false
			var walk func(any)
			walk = func(value any) {
				switch current := value.(type) {
				case map[string]any:
					if current["id"] == created.ID {
						found = true
						if current["workflow_invalidated_by_rework_id"] != source || current["workflow_instance_id"] != source || current["workflow_stage_id"] != source {
							t.Errorf("workflow history marker missing: %+v", current)
						}
					}
					for _, child := range current {
						walk(child)
					}
				case []any:
					for _, child := range current {
						walk(child)
					}
				}
			}
			walk(body)
			if !found {
				t.Fatal("test issue absent from response")
			}
		})
	}
}

func testWorkflowReworkToManualAPI(t *testing.T, runID, sourceID, stageID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE workflow_instance_stage SET position=1 WHERE id=$1`, stageID); err != nil {
		t.Fatal(err)
	}
	var manualID string
	if err := testPool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode) VALUES($1,$2,'manual_rework','Manual rework',0,'manual') RETURNING id`, testWorkspaceID, runID).Scan(&manualID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{rollback_stage_key}','"manual_rework"') WHERE id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"expected_revision": 1, "target_stage_id": manualID, "reason": "Correct manual input"}
	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.ReworkWorkflowExecution(w, workflowRequest(http.MethodPost, "/rework", body, map[string]string{"id": runID, "executionId": sourceID}))
		return w
	}
	first := post()
	var response struct {
		ReworkID    string  `json:"rework_id"`
		ExecutionID *string `json:"execution_id"`
		Attempt     *int    `json:"attempt"`
		StageID     string  `json:"stage_id"`
		Status      string  `json:"status"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil || first.Code != 200 || response.ReworkID == "" || response.ExecutionID != nil || response.Attempt != nil || response.Status != "awaiting_work" || response.StageID != manualID {
		t.Fatalf("manual rework: %d %s %v", first.Code, first.Body.String(), err)
	}
	if replay := post(); replay.Code != 200 || replay.Body.String() != first.Body.String() {
		t.Fatalf("manual replay: %d %s", replay.Code, replay.Body.String())
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1 AND workflow_stage_id=$2`, runID, manualID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("fake manual execution: %d %v", count, err)
	}
}
