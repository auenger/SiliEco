package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkflowManualRollbackInvalidatesHistory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cleanupWorkflowTestData(t)
	ctx := context.Background()
	var projectID string
	if err := testPool.QueryRow(ctx, `INSERT INTO project(workspace_id,title) VALUES($1,'Manual rollback regression') RETURNING id`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id=$1`, projectID) })
	created := httptest.NewRecorder()
	testHandler.CreateWorkflow(created, newRequest(http.MethodPost, "/api/workflows", map[string]any{"name": "Manual rollback", "project_id": projectID, "publish": true, "stages": []map[string]any{{"name": "Build"}, {"name": "Review", "rework_policy": map[string]any{"max_reworks": 1}, "retry_policy": map[string]any{"max_attempts": 2, "backoff_seconds": 0}, "gate": map[string]any{"type": "human"}, "rollback_stage_key": "stage_1"}}}))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var definition WorkflowResponse
	if err := json.Unmarshal(created.Body.Bytes(), &definition); err != nil {
		t.Fatal(err)
	}
	run := startRunForWorkflowTest(t, definition.ID)
	var firstID, secondID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM workflow_instance_stage WHERE workflow_instance_id=$1 AND position=0`, run.ID).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM workflow_instance_stage WHERE workflow_instance_id=$1 AND position=1`, run.ID).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	transition := func(body map[string]any) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		testHandler.TransitionWorkflowInstance(response, workflowRequest(http.MethodPost, "/transition", body, map[string]string{"id": run.ID}))
		return response
	}
	first := transition(map[string]any{"outcome": "approved", "note": "Original Build accepted", "expected_state_token": workflowStateTokenForTest(t, run.ID)})
	if first.Code != http.StatusOK {
		t.Fatalf("first advance: %d %s", first.Code, first.Body.String())
	}
	var oldIssue string
	if err := testPool.QueryRow(ctx, `INSERT INTO issue(workspace_id,project_id,title,status,priority,creator_type,creator_id,number,workflow_instance_id,workflow_stage_id) VALUES($1,$2,'Old manual review','done','none','member',$3,9904,$4,$5) RETURNING id`, testWorkspaceID, projectID, testUserID, run.ID, secondID).Scan(&oldIssue); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, oldIssue) })
	state := workflowStateTokenForTest(t, run.ID)
	missingReason := transition(map[string]any{"outcome": "rejected", "target_stage_id": firstID, "expected_state_token": state})
	if missingReason.Code != http.StatusBadRequest {
		t.Fatalf("rollback without reason: %d %s", missingReason.Code, missingReason.Body.String())
	}
	body := map[string]any{"outcome": "rejected", "target_stage_id": firstID, "note": "Revise the Build evidence", "expected_state_token": state}
	rolledBack := transition(body)
	if rolledBack.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rolledBack.Code, rolledBack.Body.String())
	}
	var current WorkflowInstanceResponse
	if err := json.Unmarshal(rolledBack.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.CurrentStageID == nil || *current.CurrentStageID != firstID || current.StateToken == state {
		t.Fatalf("rollback did not change state: %+v", current)
	}
	if replay := transition(body); replay.Code != http.StatusConflict {
		t.Fatalf("old rollback was replayed: %d", replay.Code)
	}
	var recordID, issueMarker, decisionMarker, oldStatus string
	if err := testPool.QueryRow(ctx, `SELECT id FROM workflow_rework WHERE workflow_instance_id=$1 AND source_stage_id=$2 AND target_stage_id=$3 AND source_execution_id IS NULL AND source_state_token=$4 AND reason='Revise the Build evidence'`, run.ID, secondID, firstID, state).Scan(&recordID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT workflow_invalidated_by_rework_id,status FROM issue WHERE id=$1`, oldIssue).Scan(&issueMarker, &oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `SELECT invalidated_by_rework_id FROM workflow_gate_decision WHERE workflow_instance_id=$1 AND from_stage_id=$2 AND outcome='approved'`, run.ID, firstID).Scan(&decisionMarker); err != nil {
		t.Fatal(err)
	}
	if issueMarker != recordID || decisionMarker != recordID || oldStatus != "done" {
		t.Fatalf("manual history not retained and invalidated: %s %s %s", issueMarker, decisionMarker, oldStatus)
	}
	var freshIssue string
	if err := testPool.QueryRow(ctx, `INSERT INTO issue(workspace_id,project_id,title,status,priority,creator_type,creator_id,number,workflow_instance_id,workflow_stage_id) VALUES($1,$2,'Fresh Build evidence','in_progress','none','member',$3,9905,$4,$5) RETURNING id`, testWorkspaceID, projectID, testUserID, run.ID, firstID).Scan(&freshIssue); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, freshIssue) })
	approve := map[string]any{"outcome": "approved", "expected_state_token": current.StateToken}
	if blocked := transition(approve); blocked.Code != http.StatusConflict {
		t.Fatalf("fresh incomplete delivery accepted: %d", blocked.Code)
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, freshIssue); err != nil {
		t.Fatal(err)
	}
	if advanced := transition(approve); advanced.Code != http.StatusOK {
		t.Fatalf("fresh manual delivery blocked: %d %s", advanced.Code, advanced.Body.String())
	}
	var policy string
	if err := testPool.QueryRow(ctx, `SELECT rework_policy->>'max_reworks' FROM workflow_instance_stage WHERE id=$1`, secondID).Scan(&policy); err != nil || policy != "1" {
		t.Fatalf("Run lost template rework policy: %q %v", policy, err)
	}
	var retryPolicy string
	if err := testPool.QueryRow(ctx, `SELECT retry_policy->>'max_attempts' FROM workflow_instance_stage WHERE id=$1`, secondID).Scan(&retryPolicy); err != nil || retryPolicy != "2" {
		t.Fatalf("Run lost template retry policy: %q %v", retryPolicy, err)
	}
	limitedState := workflowStateTokenForTest(t, run.ID)
	limited := transition(map[string]any{"outcome": "rejected", "target_stage_id": firstID, "note": "Another rework", "expected_state_token": limitedState})
	if limited.Code != http.StatusConflict {
		t.Fatalf("exhausted manual rework budget: %d %s", limited.Code, limited.Body.String())
	}
	if after := workflowStateTokenForTest(t, run.ID); after != limitedState {
		t.Fatal("denied rework changed the Run state")
	}
	history := httptest.NewRecorder()
	testHandler.GetWorkflowInstance(history, workflowRequest(http.MethodGet, "/run", nil, map[string]string{"id": run.ID}))
	if history.Code != http.StatusOK {
		t.Fatalf("read history: %d %s", history.Code, history.Body.String())
	}
	var viewed WorkflowInstanceResponse
	if err := json.Unmarshal(history.Body.Bytes(), &viewed); err != nil {
		t.Fatal(err)
	}
	if len(viewed.Reworks) != 1 || viewed.Reworks[0].ID != recordID || viewed.Reworks[0].SourceExecutionID != nil || viewed.Reworks[0].Reason != "Revise the Build evidence" {
		t.Fatalf("API lost manual rework history: %+v", viewed.Reworks)
	}
	var records, executions int
	if err := testPool.QueryRow(ctx, `SELECT (SELECT count(*) FROM workflow_rework WHERE workflow_instance_id=$1),(SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1)`, run.ID).Scan(&records, &executions); err != nil {
		t.Fatal(err)
	}
	if records != 1 || executions != 0 {
		t.Fatalf("manual rollback fabricated agent execution or repeated invalidation: records=%d executions=%d", records, executions)
	}
	deleted := httptest.NewRecorder()
	testHandler.DeleteProject(deleted, workflowRequest(http.MethodDelete, "/api/projects/"+projectID, nil, map[string]string{"id": projectID}))
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete project: %d %s", deleted.Code, deleted.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_rework WHERE workflow_instance_id=$1`, run.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("deleted project retained rework records: %d %v", records, err)
	}
	var staleMarkers int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id=$1 AND workflow_invalidated_by_rework_id IS NOT NULL`, oldIssue).Scan(&staleMarkers); err != nil || staleMarkers != 0 {
		t.Fatalf("deleted Run left immutable issue pointer: %d %v", staleMarkers, err)
	}

}
