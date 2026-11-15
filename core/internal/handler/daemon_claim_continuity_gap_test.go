package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// claimContinuityGapProbe decodes just the continuity-gap fields off a claim
// response so the two SILI-5305 disclosure paths can be asserted end-to-end.
type claimContinuityGapProbe struct {
	AgentRun *struct {
		ID                            string `json:"id"`
		PriorSessionID                string `json:"prior_session_id"`
		PriorSessionResumeUnavailable bool   `json:"prior_session_resume_unavailable"`
	} `json:"run"`
}

func claimOneRunForRuntime(t *testing.T, runtimeID, daemonID string) claimContinuityGapProbe {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimRunByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var probe claimContinuityGapProbe
	if err := json.NewDecoder(w.Body).Decode(&probe); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if probe.AgentRun == nil {
		t.Fatal("expected a claimed task in the response")
	}
	return probe
}

// TestClaimTaskByRuntime_ChatRolloutMissingDisclosesGap is the claim-response
// half of SILI-5305 Must-fix 2 for chat: when the most recent terminal task on a
// chat session withheld its Codex session (rollout missing), the next chat claim
// resumes the older pointer but MUST still set prior_session_resume_unavailable
// so the run discloses the gap instead of silently continuing.
func TestClaimRunByRuntime_ChatRolloutMissingDisclosesGap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "ChatGapClaimAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	// Chat session that still carries a good resume pointer from an earlier turn.
	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status, runtime_id, session_id, work_dir)
		VALUES ($1, $2, $3, 'gap chat', 'active', $4, 'OLD-GOOD-CHAT-SESSION', '/tmp/chat')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID) })

	// Most recent terminal turn on this session withheld its Codex session.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, started_at, completed_at, chat_session_id, session_rollout_missing)
		VALUES ($1, $2, 'completed', 0, now() - interval '1 minute', now() - interval '1 minute', $3, TRUE)
	`, agentID, runtimeID, sessionID); err != nil {
		t.Fatalf("insert withheld chat run: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'next turn')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, chat_session_id)
		VALUES ($1, $2, 'queued', 1000, $3) RETURNING id
	`, agentID, runtimeID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create chat follow-up run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	probe := claimOneRunForRuntime(t, runtimeID, "chat-gap-claim-test")
	if !probe.AgentRun.PriorSessionResumeUnavailable {
		t.Fatalf("expected chat claim to disclose the continuity gap; response task=%+v", *probe.AgentRun)
	}
}

// TestClaimTaskByRuntime_RerunSourceRolloutMissingDisclosesGap is the
// claim-response half of SILI-5305 Must-fix 2 for manual rerun: when the exact
// source task withheld its Codex session, the rerun has nothing resumable from
// it and MUST disclose the gap rather than silently start fresh.
func TestClaimRunByRuntime_RerunSourceRolloutMissingDisclosesGap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "RerunGapClaimAgent", []byte("[]"))
	runtimeID := handlerTestRuntimeID(t)

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, number)
		VALUES ($1, 'rerun gap task', 'todo', 'none', 'member', $2, 'agent', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM task WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Source task whose Codex session was withheld (rollout missing).
	var srcID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, started_at, completed_at, session_rollout_missing)
		VALUES ($1, $2, $3, 'completed', 0, now() - interval '2 minutes', now() - interval '2 minutes', TRUE)
		RETURNING id
	`, agentID, runtimeID, taskID).Scan(&srcID); err != nil {
		t.Fatalf("insert source run: %v", err)
	}

	// A manual rerun of that source task, queued to claim (rerun carries
	// force_fresh_session=true; the disclosure must still fire from the source).
	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, rerun_of_run_id, force_fresh_session)
		VALUES ($1, $2, $3, 'queued', 1000, $4, TRUE) RETURNING id
	`, agentID, runtimeID, taskID, srcID).Scan(&runID); err != nil {
		t.Fatalf("create rerun run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	probe := claimOneRunForRuntime(t, runtimeID, "rerun-gap-claim-test")
	if !probe.AgentRun.PriorSessionResumeUnavailable {
		t.Fatalf("expected rerun claim to disclose the source task's continuity gap; response task=%+v", *probe.AgentRun)
	}
}
