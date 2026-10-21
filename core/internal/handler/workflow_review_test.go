package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func testWorkflowHumanReviewAPI(t *testing.T, runID, executionID string) {
	t.Helper()
	ctx := context.Background()
	var issueID, taskID, issueStatus, taskStatus string
	var config []byte
	if err := testPool.QueryRow(ctx, `SELECT e.main_issue_id,e.main_task_id,i.status,t.status,e.config_snapshot FROM workflow_stage_execution e JOIN issue i ON i.id=e.main_issue_id JOIN agent_task_queue t ON t.id=e.main_task_id WHERE e.id=$1`, executionID).Scan(&issueID, &taskID, &issueStatus, &taskStatus, &config); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Restore this shared fixture before its independent deletion tests.
		if _, err := testPool.Exec(ctx, `UPDATE workflow_instance SET status='active',completed_at=NULL WHERE id=$1`, runID); err != nil {
			t.Error(err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=$2,status='awaiting_review',acceptance=NULL,finished_at=NULL WHERE id=$1`, executionID, config); err != nil {
			t.Error(err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE issue SET status=$2 WHERE id=$1`, issueID, issueStatus); err != nil {
			t.Error(err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status=$2 WHERE id=$1`, taskID, taskStatus); err != nil {
			t.Error(err)
		}
	}()
	if _, err := testPool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',jsonb_build_object('type','human','decider',$2::text)) WHERE id=$1`, executionID, testUserID); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": runID, "executionId": executionID}
	read := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		testHandler.GetWorkflowHumanReview(r, workflowRequest(http.MethodGet, "/review", nil, params))
		return r
	}
	if r := read(); r.Code != http.StatusConflict {
		t.Fatalf("incomplete review: %d %s", r.Code, r.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, issueID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	r := read()
	var basis WorkflowHumanReviewResponse
	if err := json.Unmarshal(r.Body.Bytes(), &basis); err != nil {
		t.Fatal(err)
	}
	if r.Code != http.StatusOK || len(basis.BasisHash) != 64 || len(basis.Issues) != 1 || len(basis.Calls) != 1 || strings.Contains(r.Body.String(), "storage_key") || strings.Contains(r.Body.String(), "config_snapshot") {
		t.Fatalf("review response: %d %s", r.Code, r.Body.String())
	}
	body := map[string]any{"expected_result_revision": basis.ResultRevision, "expected_basis_hash": basis.BasisHash, "outcome": "approved", "note": "Reviewed"}
	post := func(body any) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		testHandler.RecordWorkflowHumanDecision(r, workflowRequest(http.MethodPost, "/decisions", body, params))
		return r
	}
	for _, bad := range []any{map[string]any{}, map[string]any{"outcome": "approved", "actor_id": testUserID}, map[string]any{"expected_result_revision": 1, "expected_basis_hash": "invalid", "outcome": "approved"}} {
		if r := post(bad); r.Code != http.StatusBadRequest {
			t.Fatalf("invalid request: %d %s", r.Code, r.Body.String())
		}
	}
	agentRequest := workflowRequest(http.MethodPost, "/decisions", body, params)
	agentRequest.Header.Set("X-Agent-ID", uuid.NewString())
	agentResult := httptest.NewRecorder()
	testHandler.RecordWorkflowHumanDecision(agentResult, agentRequest)
	if agentResult.Code != http.StatusForbidden {
		t.Fatalf("agent became human: %d %s", agentResult.Code, agentResult.Body.String())
	}
	approved := post(body)
	var decision WorkflowGateDecisionResponse
	if err := json.Unmarshal(approved.Body.Bytes(), &decision); err != nil {
		t.Fatal(err)
	}
	if approved.Code != http.StatusOK || decision.ExecutionID == nil || *decision.ExecutionID != executionID || decision.BasisHash == nil || *decision.BasisHash != basis.BasisHash || strings.Contains(approved.Body.String(), "basis_snapshot") {
		t.Fatalf("decision response: %d %s", approved.Code, approved.Body.String())
	}
	var status, executionStatus string
	if err := testPool.QueryRow(ctx, `SELECT r.status,e.status FROM workflow_instance r JOIN workflow_stage_execution e ON e.workflow_instance_id=r.id WHERE e.id=$1`, executionID).Scan(&status, &executionStatus); err != nil || status != "completed" || executionStatus != "succeeded" {
		t.Fatalf("approval did not complete delivery: %s %s %v", status, executionStatus, err)
	}
	repeated := post(body)
	var duplicate WorkflowGateDecisionResponse
	if err := json.Unmarshal(repeated.Body.Bytes(), &duplicate); err != nil || repeated.Code != http.StatusOK || duplicate.ID != decision.ID {
		t.Fatalf("retry: %d %s %v", repeated.Code, repeated.Body.String(), err)
	}
	body["expected_result_revision"] = 2
	if r := post(body); r.Code != http.StatusConflict {
		t.Fatalf("old review approved new result: %d %s", r.Code, r.Body.String())
	}
	params["id"] = uuid.NewString()
	if r := read(); r.Code != http.StatusNotFound {
		t.Fatalf("wrong Run: %d %s", r.Code, r.Body.String())
	}
}
