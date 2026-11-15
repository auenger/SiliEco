package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/service"
)

func testWorkflowAgentReviewAPI(t *testing.T, workflowRunID, executionID string) {
	t.Helper()
	ctx := context.Background()
	var taskID, mainRunID, agentID, taskStatus, runStatus string
	var config []byte
	if err := testPool.QueryRow(ctx, `SELECT e.main_task_id,e.main_run_id,e.executor_id,i.status,t.status,e.config_snapshot FROM workflow_stage_execution e JOIN task i ON i.id=e.main_task_id JOIN agent_run t ON t.id=e.main_run_id WHERE e.id=$1`, executionID).Scan(&taskID, &mainRunID, &agentID, &taskStatus, &runStatus, &config); err != nil {
		t.Fatal(err)
	}
	defer func() {
		testPool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=$2 WHERE id=$1`, executionID, config)
		testPool.Exec(ctx, `UPDATE task SET status=$2 WHERE id=$1`, taskID, taskStatus)
		testPool.Exec(ctx, `UPDATE agent_run SET status=$2 WHERE id=$1`, mainRunID, runStatus)
	}()
	if _, err := testPool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',$2::jsonb) WHERE id=$1`, executionID, fmt.Sprintf(`{"type":"agent","decider":%q}`, agentID)); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE task SET status='done' WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status='completed' WHERE id=$1`, mainRunID); err != nil {
		t.Fatal(err)
	}
	svc := service.WorkflowExecutionService{Queries: testHandler.Queries, TxStarter: testHandler.TxStarter, AgentRunService: testHandler.AgentRunService}
	request, err := svc.PrepareAgentReview(ctx, parseUUID(testWorkspaceID), parseUUID(workflowRunID), parseUUID(executionID))
	if err != nil {
		t.Fatal(err)
	}
	request, err = svc.DispatchAgentReview(ctx, parseUUID(testWorkspaceID), request.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		testPool.Exec(ctx, `DELETE FROM workflow_gate_decision WHERE execution_id=$1 AND review_step='agent'`, executionID)
		testPool.Exec(ctx, `DELETE FROM workflow_review_recovery WHERE request_id=$1`, request.ID)
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE workflow_review_request_id=$1`, request.ID)
		testPool.Exec(ctx, `DELETE FROM workflow_review_request WHERE id=$1`, request.ID)
	}()
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status='running' WHERE id=$1`, request.RunID); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": workflowRunID, "executionId": executionID, "requestId": uuidToString(request.ID)}
	call := func(post bool, agentRun string, body any) *httptest.ResponseRecorder {
		method := http.MethodGet
		if post {
			method = http.MethodPost
		}
		r := workflowRequest(method, "/review", body, params)
		if agentRun != "" {
			r.Header.Set("X-Agent-ID", agentID)
			r.Header.Set("X-Run-ID", agentRun)
		}
		w := httptest.NewRecorder()
		if post {
			testHandler.RecordWorkflowAgentDecision(w, r)
		} else {
			testHandler.GetWorkflowAgentReview(w, r)
		}
		return w
	}
	if w := call(false, "", nil); w.Code != http.StatusForbidden {
		t.Fatalf("member read reviewer request: %d %s", w.Code, w.Body.String())
	}
	if w := call(false, mainRunID, nil); w.Code != http.StatusForbidden {
		t.Fatalf("executor read reviewer request: %d %s", w.Code, w.Body.String())
	}
	runID := uuidToString(request.RunID)
	if w := call(false, runID, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "basis_snapshot") || !strings.Contains(w.Body.String(), request.BasisHash) {
		t.Fatalf("review read: %d %s", w.Code, w.Body.String())
	}
	body := map[string]any{"expected_result_revision": request.ResultRevision, "expected_basis_hash": request.BasisHash, "outcome": "approved"}
	if w := call(true, mainRunID, body); w.Code != http.StatusForbidden {
		t.Fatalf("executor decided: %d %s", w.Code, w.Body.String())
	}
	for _, invalid := range []map[string]any{{"actor_id": agentID}, {"expected_result_revision": 1, "expected_basis_hash": "invalid", "outcome": "approved"}} {
		if w := call(true, runID, invalid); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid payload: %d %s", w.Code, w.Body.String())
		}
	}
	stale := map[string]any{"expected_result_revision": request.ResultRevision + 1, "expected_basis_hash": request.BasisHash, "outcome": "approved"}
	if w := call(true, runID, stale); w.Code != http.StatusConflict {
		t.Fatalf("stale revision accepted: %d %s", w.Code, w.Body.String())
	}
	w := call(true, runID, body)
	var decision WorkflowGateDecisionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &decision); err != nil || w.Code != http.StatusOK || decision.ReviewStep == nil || *decision.ReviewStep != "agent" {
		t.Fatalf("review decision: %d %s %v", w.Code, w.Body.String(), err)
	}
	if w := call(true, runID, body); w.Code != http.StatusOK {
		t.Fatalf("decision retry: %d %s", w.Code, w.Body.String())
	}
	params["executionId"] = workflowRunID
	if w := call(true, runID, body); w.Code != http.StatusNotFound {
		t.Fatalf("wrong execution accepted: %d %s", w.Code, w.Body.String())
	}
	testWorkflowReviewRecoveryAPI(t, workflowRunID, executionID, uuidToString(request.ID), runID, request.ResultRevision, request.BasisHash, agentID)
}
