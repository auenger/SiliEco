package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testWorkflowInputCorrectionAPI(t *testing.T, runID string) {
	t.Helper()
	ctx := context.Background()
	var executionID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM workflow_stage_execution WHERE workflow_instance_id=$1`, runID).Scan(&executionID); err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": runID, "executionId": executionID}
	post := func(body any, agent bool) *httptest.ResponseRecorder {
		r := workflowRequest(http.MethodPost, "/inputs", body, params)
		if agent {
			r.Header.Set("X-Agent-ID", testUserID)
		}
		w := httptest.NewRecorder()
		testHandler.CorrectWorkflowExecutionInputs(w, r)
		return w
	}
	body := map[string]any{"expected_inputs": map[string]any{}, "inputs": map[string]any{"goal": "Recovered delivery"}}
	if w := post(body, true); w.Code != http.StatusForbidden {
		t.Fatalf("agent corrected member input: %d %s", w.Code, w.Body.String())
	}
	if w := post(map[string]any{"inputs": nil}, false); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid payload: %d %s", w.Code, w.Body.String())
	}
	if w := post(map[string]any{"expected_inputs": map[string]any{}, "inputs": map[string]any{"goal": 7}}, false); w.Code != http.StatusConflict {
		t.Fatalf("invalid input resumed: %d %s", w.Code, w.Body.String())
	}
	if w := post(map[string]any{"expected_inputs": map[string]any{"old": true}, "inputs": body["inputs"]}, false); w.Code != http.StatusConflict {
		t.Fatalf("stale input replaced: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE execution_id=$1`, executionID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed request dispatched: %d %v", count, err)
	}
	w := post(body, false)
	var response struct {
		ExecutionID string `json:"execution_id"`
		InstanceID  string `json:"instance_id"`
		Status      string `json:"status"`
		Attempt     int    `json:"attempt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != http.StatusOK || response.ExecutionID != executionID || response.InstanceID != runID || response.Status != "pending" || response.Attempt != 1 {
		t.Fatalf("recovery: %d %s %v", w.Code, w.Body.String(), err)
	}
	if w := post(body, false); w.Code != http.StatusConflict {
		t.Fatalf("replayed correction: %d %s", w.Code, w.Body.String())
	}
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE execution_id=$1`, executionID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate dispatch: %d %v", count, err)
	}
}
