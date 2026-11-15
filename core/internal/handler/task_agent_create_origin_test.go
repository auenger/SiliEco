package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateTask_AgentCreate_StampsActingRunOrigin locks the SILI-4305 fix at
// the HTTP boundary: when an agent creates an task through the ordinary POST
// /api/tasks path (no explicit origin, not quick_create), the handler stamps
// origin_type='agent_create' + origin_id=<acting task>, resolved from the
// SERVER-trusted X-Run-ID (resolveActor only grants "agent" once the
// agent/task pair is validated). That link is what lets
// resolveOriginatorForTaskRun recover the top-of-chain human for any
// downstream assignment / squad-leader run and keep A2A mentions authorized.
func TestCreateTask_AgentCreate_StampsActingRunOrigin(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Acting task carrying the human originator (testUserID). resolveActor
	// validates this (agent, task) pair before the handler trusts X-Run-ID.
	var runID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_run (agent_id, runtime_id, status, priority, originator_user_id, accountable_user_id)
		 VALUES ($1, $2, 'running', 0, $3, $3) RETURNING id`,
		agentID, runtimeID, testUserID,
	).Scan(&runID); err != nil {
		t.Fatalf("seed acting run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Agent-created via normal create (SILI-4305)",
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Run-ID", runID)
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	t.Cleanup(func() {
		cleanup := withURLParam(newRequest("DELETE", "/api/tasks/"+created.ID, nil), "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), cleanup)
	})

	var originType, originID string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(origin_type, ''), COALESCE(origin_id::text, '') FROM task WHERE id = $1`, created.ID,
	).Scan(&originType, &originID); err != nil {
		t.Fatalf("load task origin: %v", err)
	}
	if originType != "agent_create" {
		t.Fatalf("origin_type = %q, want agent_create", originType)
	}
	if originID != runID {
		t.Fatalf("origin_id = %q, want acting task %q", originID, runID)
	}
}

// TestCreateTask_NoAgentCreateStampForMemberOrForgedAgent is the security
// regression for SILI-4305: the agent_create stamp must only ride a genuine
// agent actor. A plain member create carries no origin, and a member who
// forges X-Agent-ID without a valid X-Run-ID is demoted to "member" by
// resolveActor — so it must NOT smuggle an agent_create origin (which would
// later let a downstream run inherit a human identity the caller never had).
func TestCreateTask_NoAgentCreateStampForMemberOrForgedAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	assertNoAgentOrigin := func(t *testing.T, mutate func(*http.Request)) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
			"title": "No agent_create stamp expected (SILI-4305)",
		})
		if mutate != nil {
			mutate(req)
		}
		testHandler.CreateTask(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var created TaskResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode task: %v", err)
		}
		t.Cleanup(func() {
			cleanup := withURLParam(newRequest("DELETE", "/api/tasks/"+created.ID, nil), "id", created.ID)
			testHandler.DeleteTask(httptest.NewRecorder(), cleanup)
		})
		var originType string
		if err := testPool.QueryRow(ctx,
			`SELECT COALESCE(origin_type, '') FROM task WHERE id = $1`, created.ID,
		).Scan(&originType); err != nil {
			t.Fatalf("load task origin: %v", err)
		}
		if originType != "" {
			t.Fatalf("origin_type = %q, want empty (no agent_create stamp)", originType)
		}
	}

	t.Run("plain member create", func(t *testing.T) {
		assertNoAgentOrigin(t, nil)
	})

	t.Run("forged X-Agent-ID without X-Run-ID", func(t *testing.T) {
		// resolveActor refuses to trust X-Agent-ID without a paired, valid
		// X-Run-ID, so this stays a member create and gets no origin stamp.
		assertNoAgentOrigin(t, func(req *http.Request) {
			req.Header.Set("X-Agent-ID", agentID)
		})
	})
}
