package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// insertCommentForScopeTest inserts a member comment on taskID in the given
// workspace and returns its id.
func insertCommentForScopeTest(t *testing.T, ctx context.Context, taskID, workspaceID, content string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, $4, 'comment')
		RETURNING id
	`, taskID, workspaceID, testUserID, content).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, id) })
	return id
}

// enqueueTaskRunWithTrigger enqueues a real task for (taskID, agentID) via the
// production TaskService.EnqueueRunForTask path — the path that snapshots
// trigger_summary and resolves the originator from the triggering comment — so
// the SILI-4252 workspace scoping is exercised end-to-end rather than bypassed
// with a hand-written row. Returns the created task id.
func enqueueTaskRunWithTrigger(t *testing.T, ctx context.Context, agentID, taskID, triggerCommentID string) string {
	t.Helper()
	if _, err := testPool.Exec(ctx, `UPDATE task SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, taskID, agentID); err != nil {
		t.Fatalf("assign task before enqueue: %v", err)
	}
	task, err := testHandler.Queries.GetTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("load task for enqueue: %v", err)
	}
	agentRun, err := testHandler.AgentRunService.EnqueueRunForTask(ctx, task, parseUUID(triggerCommentID))
	if err != nil {
		t.Fatalf("EnqueueRunForTask: %v", err)
	}
	runID := uuidToString(agentRun.ID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })
	return runID
}

// claimTriggerFieldsForTest claims the next task for runtimeID and returns the
// triggering comment content + summary the claim response carried (both empty
// when absent — omitempty on the wire) plus the raw response body.
func claimTriggerFieldsForTest(t *testing.T, runtimeID string) (runID, triggerContent, triggerSummary, raw string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/runs/claim", nil,
		testWorkspaceID, "comment-workspace-scope")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimRunByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimRunByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AgentRun *struct {
			ID                    string `json:"id"`
			TriggerCommentContent string `json:"trigger_comment_content"`
			TriggerSummary        string `json:"trigger_summary"`
		} `json:"run"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if resp.AgentRun == nil {
		return "", "", "", w.Body.String()
	}
	return resp.AgentRun.ID, resp.AgentRun.TriggerCommentContent, resp.AgentRun.TriggerSummary, w.Body.String()
}

// TestClaimDeliversSameWorkspaceTriggerComment is the positive path: a triggering
// comment in the task's own workspace must still be snapshotted into
// trigger_summary at enqueue and embedded (content + summary) in the claim
// response. The SILI-4252 workspace-scoped lookups must not regress delivery.
func TestClaimDeliversSameWorkspaceTriggerComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "same-ws trigger runtime")
	agentID, taskID := createClaimReclaimAgentAndTask(t, ctx, runtimeID, "same-ws trigger agent")

	const body = "same-workspace trigger body ABC123"
	commentID := insertCommentForScopeTest(t, ctx, taskID, testWorkspaceID, body)

	want := enqueueTaskRunWithTrigger(t, ctx, agentID, taskID, commentID)
	got, content, summary, raw := claimTriggerFieldsForTest(t, runtimeID)
	if got != want {
		t.Fatalf("claimed task id = %q, want %q: %s", got, want, raw)
	}
	if content != body {
		t.Fatalf("trigger_comment_content = %q, want same-workspace body %q", content, body)
	}
	if summary != body {
		t.Fatalf("trigger_summary = %q, want same-workspace body %q", summary, body)
	}
}

// TestClaimDoesNotLeakForeignWorkspaceTriggerCommentOrSummary is the SILI-4252
// guard, exercised through the real enqueue + claim path. A task whose
// trigger_comment_id points at a comment owned by a DIFFERENT workspace must
// leak neither the full body (claim-time fetch) nor the truncated summary
// (enqueue-time snapshot), and must not inherit an originator from it.
func TestClaimDoesNotLeakForeignWorkspaceTriggerCommentOrSummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "foreign trigger runtime")
	agentID, taskID := createClaimReclaimAgentAndTask(t, ctx, runtimeID, "foreign trigger agent")

	// A comment that lives entirely in a DIFFERENT workspace (its own task).
	otherWS := createOtherTestWorkspace(t)
	var foreignTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'foreign task', 'in_progress', 'none', $2, 'member',
			(SELECT COALESCE(MAX(number), 90000) + 1 FROM task WHERE workspace_id = $1), 0)
		RETURNING id
	`, otherWS, testUserID).Scan(&foreignTaskID); err != nil {
		t.Fatalf("insert foreign-workspace task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, foreignTaskID) })

	const secret = "FOREIGN-WORKSPACE-SECRET-DO-NOT-LEAK"
	foreignCommentID := insertCommentForScopeTest(t, ctx, foreignTaskID, otherWS, secret)

	// Enqueue through the production path: this is where trigger_summary is
	// snapshotted and the originator resolved. Both must fail closed.
	runID := enqueueTaskRunWithTrigger(t, ctx, agentID, taskID, foreignCommentID)

	// Stored row: neither the summary nor the originator may come from the
	// foreign comment.
	var storedSummary pgtype.Text
	var storedOriginator pgtype.UUID
	if err := testPool.QueryRow(ctx,
		`SELECT trigger_summary, originator_user_id FROM agent_run WHERE id = $1`, runID,
	).Scan(&storedSummary, &storedOriginator); err != nil {
		t.Fatalf("read stored run: %v", err)
	}
	if storedSummary.Valid {
		t.Fatalf("stored trigger_summary must be NULL for a foreign-workspace comment, got %q", storedSummary.String)
	}
	if storedOriginator.Valid {
		t.Fatalf("stored originator_user_id must be NULL for a foreign-workspace comment, got %s", uuidToString(storedOriginator))
	}

	// Wire: the claim response must carry neither the body nor the summary.
	got, content, summary, raw := claimTriggerFieldsForTest(t, runtimeID)
	if got != runID {
		t.Fatalf("claimed task id = %q, want %q: %s", got, runID, raw)
	}
	if content != "" {
		t.Fatalf("trigger_comment_content must be empty for a foreign-workspace comment, got %q", content)
	}
	if summary != "" {
		t.Fatalf("trigger_summary must be empty for a foreign-workspace comment, got %q", summary)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("claim response leaked foreign-workspace comment text:\n%s", raw)
	}
}
