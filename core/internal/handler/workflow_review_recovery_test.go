package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testWorkflowReviewRecoveryAPI(t *testing.T, runID, executionID, requestID, failedID string, revision int32, basis, agentID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='failed',attempt=3,max_attempts=3,completed_at=now(),failure_reason='agent_error.provider_network',error='PRIVATE_REVIEW_ERROR',session_id='PRIVATE_REVIEW_SESSION' WHERE id=$1`, failedID); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": runID, "executionId": executionID, "requestId": requestID}
	statusRequest := func(agent bool) *httptest.ResponseRecorder {
		r := workflowRequest(http.MethodGet, "/review-status", nil, params)
		if agent {
			r.Header.Set("X-Agent-ID", agentID)
		}
		w := httptest.NewRecorder()
		testHandler.GetWorkflowReviewStatus(w, r)
		return w
	}
	if denied := statusRequest(true); denied.Code != http.StatusForbidden {
		t.Fatalf("agent entered member review status: %d", denied.Code)
	}
	status := statusRequest(false)
	if status.Code != http.StatusOK {
		t.Fatalf("review status: %d %s", status.Code, status.Body.String())
	}
	var before WorkflowReviewStatusResponse
	if err := json.Unmarshal(status.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if before.Phase != "failed" || !before.CanRecover || before.Call == nil || before.Call.ID != failedID || before.RecoveryAttempts != 3 {
		t.Fatalf("recovery state missing: %+v", before)
	}
	for _, secret := range []string{"PRIVATE_REVIEW_ERROR", "PRIVATE_REVIEW_SESSION", "basis_snapshot", "runtime_mcp_overlay"} {
		if strings.Contains(status.Body.String(), secret) {
			t.Fatalf("review status exposed %s", secret)
		}
	}
	body := map[string]any{"failed_task_id": failedID, "expected_result_revision": revision, "expected_basis_hash": basis, "reason": "Runtime repaired; review again"}
	post := func(payload any, agent bool) *httptest.ResponseRecorder {
		r := workflowRequest(http.MethodPost, "/recover", payload, params)
		if agent {
			r.Header.Set("X-Agent-ID", agentID)
			r.Header.Set("X-Task-ID", failedID)
		}
		w := httptest.NewRecorder()
		testHandler.RecoverWorkflowAgentReview(w, r)
		return w
	}
	if denied := post(body, true); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer recovered itself: %d", denied.Code)
	}
	if invalid := post(map[string]any{"actor_id": agentID}, false); invalid.Code != http.StatusBadRequest {
		t.Fatalf("client actor accepted: %d", invalid.Code)
	}
	stale := map[string]any{"failed_task_id": failedID, "expected_result_revision": revision + 1, "expected_basis_hash": basis, "reason": "Runtime repaired"}
	if response := post(stale, false); response.Code != http.StatusConflict {
		t.Fatalf("stale recovery accepted: %d %s", response.Code, response.Body.String())
	}
	recovered := post(body, false)
	if recovered.Code != http.StatusOK {
		t.Fatalf("recover review: %d %s", recovered.Code, recovered.Body.String())
	}
	var result struct {
		RecoveryID      string `json:"recovery_id"`
		TaskID          string `json:"task_id"`
		AllowedAttempts int32  `json:"allowed_attempts"`
	}
	if err := json.Unmarshal(recovered.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RecoveryID == "" || result.TaskID == "" || result.TaskID == failedID || result.AllowedAttempts != 3 {
		t.Fatalf("fresh recovery identity missing: %+v", result)
	}
	if replay := post(body, false); replay.Code != http.StatusOK || replay.Body.String() != recovered.Body.String() {
		t.Fatalf("recovery replay changed identity: %d %s", replay.Code, replay.Body.String())
	}
	queued := statusRequest(false)
	var after WorkflowReviewStatusResponse
	if err := json.Unmarshal(queued.Body.Bytes(), &after); err != nil || queued.Code != http.StatusOK || after.Phase != "queued" || after.CanRecover || after.Call == nil || after.Call.ID != result.TaskID {
		t.Fatalf("fresh review status wrong: %d %+v %v", queued.Code, after, err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, result.TaskID); err != nil {
		t.Fatal(err)
	}
	missing := statusRequest(false)
	if err := json.Unmarshal(missing.Body.Bytes(), &after); err != nil || after.Phase != "missing_decision" || !after.CanRecover {
		t.Fatalf("missing decision not recoverable: %s %v", missing.Body.String(), err)
	}
	body["failed_task_id"] = result.TaskID
	body["reason"] = "The completed review did not submit a decision"
	if fresh := post(body, false); fresh.Code != http.StatusOK {
		t.Fatalf("recover incomplete review: %d %s", fresh.Code, fresh.Body.String())
	}

}
