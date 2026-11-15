package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type batchClaimReceiptResponse struct {
	Runs []struct {
		ID                  string   `json:"id"`
		RuntimeID           string   `json:"runtime_id"`
		AuthToken           string   `json:"auth_token"`
		DeliveredCommentIDs []string `json:"delivered_comment_ids"`
	} `json:"runs"`
}

// TestClaimTasksByRuntime_MaxTasksZeroClaimsNothing pins the SILI-4257 review
// fix: max_runs=0 is a valid "no free slots" poll that must claim nothing —
// it must NOT be coerced to 1 (which would dispatch a task the daemon can't run
// and strand it until stale reclaim).
func TestClaimRunsByRuntime_MaxRunsZeroClaimsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch max0 rt")
	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "Batch max0 agent")
	runID := seedQueuedTaskRun(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 0)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 0 {
		t.Fatalf("max_runs=0 claimed %d tasks, want 0", len(resp.Runs))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued", status)
	}
}

// TestClaimTasksByRuntime_MaxTasksNegativeIsBadRequest pins that a negative
// max_runs is rejected rather than silently coerced.
func TestClaimRunsByRuntime_MaxRunsNegativeIsBadRequest(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	rt := createClaimReclaimRuntime(t, context.Background(), "Batch neg rt")
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, -1)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative max_runs, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimTasksByRuntime_SkipsInvalidRuntimeID pins the SILI-4257 review fix:
// a malformed runtime_id must be skipped (non-panicking parse), not turned into
// a 500 — and a valid runtime in the same request is still claimed.
func TestClaimRunsByRuntime_SkipsInvalidRuntimeID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch badid rt")
	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "Batch badid agent")
	seedQueuedTaskRun(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{"not-a-uuid", rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (invalid id skipped, not 500), got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 1 || resp.Runs[0].RuntimeID != rt {
		t.Fatalf("expected the valid runtime's task to be claimed despite the invalid id, got %+v", resp.Runs)
	}
}

// seedCommentBackedQueuedTask inserts a queued task triggered by a real comment
// on its task, returning (runID, commentID).
func seedCommentBackedQueuedRun(t *testing.T, ctx context.Context, agentID, runtimeID, taskID string) (string, string) {
	t.Helper()
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please handle this')
		RETURNING id
	`, testWorkspaceID, taskID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, trigger_comment_id, status, priority)
		VALUES ($1, $2, $3, $4, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, taskID, commentID).Scan(&runID); err != nil {
		t.Fatalf("seed comment-backed run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_run WHERE id = $1`, runID) })
	return runID, commentID
}

func assertCommentDelivered(t *testing.T, ctx context.Context, runID, commentID string) {
	t.Helper()
	var member bool
	if err := testPool.QueryRow(ctx, `
		SELECT $1 = ANY(delivered_comment_ids) FROM agent_run WHERE id = $2
	`, commentID, runID).Scan(&member); err != nil {
		t.Fatalf("read delivered_comment_ids: %v", err)
	}
	if !member {
		t.Fatalf("trigger comment %s not persisted in task %s delivered_comment_ids", commentID, runID)
	}
}

// TestClaimTasksByRuntime_PersistsCommentDeliveryReceipt pins the SILI-4257
// must-fix: the batch path routes through FinalizeTaskClaim, so a comment-backed
// task claimed via batch persists the delivered_comment_ids receipt AND returns
// it in the response.
func TestClaimRunsByRuntime_PersistsCommentDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch receipt rt")
	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "Batch receipt agent")
	runID, commentID := seedCommentBackedQueuedRun(t, ctx, a, rt, i)

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 1 {
		t.Fatalf("claimed %d tasks, want 1: %s", len(resp.Runs), w.Body.String())
	}
	found := false
	for _, id := range resp.Runs[0].DeliveredCommentIDs {
		if id == commentID {
			found = true
		}
	}
	if !found {
		t.Fatalf("response delivered_comment_ids %v missing trigger comment %s", resp.Runs[0].DeliveredCommentIDs, commentID)
	}
	assertCommentDelivered(t, ctx, runID, commentID)
}

// TestClaimTasksByRuntime_StaleReclaimRecordsDeliveryReceipt pins that a
// comment-backed task recovered via the batch reclaim path (dispatched, never
// started, past the recovery window) is re-finalized so its delivery receipt is
// recorded on the replacement claim.
func TestClaimRunsByRuntime_StaleReclaimRecordsDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Batch stale-receipt rt")
	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "Batch stale-receipt agent")

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'stale reclaim comment')
		RETURNING id
	`, testWorkspaceID, i, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	// Stale dispatched, comment-backed, never started, past the 90s window.
	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, trigger_comment_id, status, priority, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'dispatched', 0, now() - interval '120 seconds', NULL)
		RETURNING id
	`, a, rt, i, commentID).Scan(&runID); err != nil {
		t.Fatalf("seed stale dispatched comment run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_run WHERE id = $1`, runID) })

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 1 || resp.Runs[0].ID != runID {
		t.Fatalf("expected the stale task %s reclaimed, got %+v", runID, resp.Runs)
	}
	assertCommentDelivered(t, ctx, runID, commentID)
}

// TestClaimTasksByRuntime_SkipsRuntimeOwnedByAnotherDaemon pins the SILI-4257
// review must-fix: a daemon must not batch-claim a task routed to a runtime
// bound to a DIFFERENT daemon, even in the same workspace. The runtime is
// skipped and its task stays queued for the owning machine.
func TestClaimRunsByRuntime_SkipsRuntimeOwnedByAnotherDaemon(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Runtime bound to a different daemon in the same (handler-test) workspace.
	var rt string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'other-daemon-machine', 'Other daemon RT', 'cloud', 'handler_test_runtime', 'online', 'x', '{}'::jsonb, now(), 'private', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&rt); err != nil {
		t.Fatalf("create other-daemon runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, rt) })

	a, i := createClaimReclaimAgentAndTask(t, ctx, rt, "Other daemon agent")
	runID := seedQueuedTaskRun(t, ctx, a, rt, i)

	// postBatchClaim sends daemon_id = batchClaimTestDaemonID ("batch-claim-review"),
	// which differs from the runtime's "other-daemon-machine".
	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Runs) != 0 {
		t.Fatalf("daemon-A claimed %d tasks from a runtime owned by daemon-B, want 0", len(resp.Runs))
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_run WHERE id = $1`, runID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %s, want still queued for the owning daemon", status)
	}
}

// TestClaimTasksByRuntime_RequiresDaemonID pins that the batch claim rejects a
// request with no daemon_id rather than falling back to workspace-only scoping.
func TestClaimRunsByRuntime_RequiresDaemonID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runs/claim",
		map[string]any{"runtime_ids": []string{}, "max_runs": 5},
		testWorkspaceID, batchClaimTestDaemonID)
	testHandler.ClaimRunsByRuntime(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when daemon_id is missing, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClaimTasksByRuntime_RepairsStaleCommentPlan pins the SILI-4257 review
// must-fix: when a claimed task's trigger comment was deleted (only coalesced
// survivors remain), the batch path must NOT finalize+dispatch it (which would
// silently drop the surviving comment). Instead it cancels the stale task,
// omits it from the batch, and replays the surviving comment as a fresh plan.
func TestClaimRunsByRuntime_RepairsStaleCommentPlan(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	rt := createClaimReclaimRuntime(t, ctx, "Stale plan rt")
	agentID, taskID := createClaimReclaimAgentAndTask(t, ctx, rt, "Stale plan agent")
	// Assign the task to the agent so the surviving comment re-routes to it.
	if _, err := testPool.Exec(ctx, `UPDATE task SET assignee_type='agent', assignee_id=$1 WHERE id=$2`, agentID, taskID); err != nil {
		t.Fatalf("assign task: %v", err)
	}

	// A surviving member comment on the task.
	var survivorID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'please still handle this')
		RETURNING id
	`, testWorkspaceID, taskID, testUserID).Scan(&survivorID); err != nil {
		t.Fatalf("seed survivor comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, survivorID) })

	// Stale plan: trigger_comment_id NULL, only coalesced survivor remains.
	var staleID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, coalesced_comment_ids)
		VALUES ($1, $2, $3, 'queued', 0, ARRAY[$4]::uuid[])
		RETURNING id
	`, agentID, rt, taskID, survivorID).Scan(&staleID); err != nil {
		t.Fatalf("seed stale run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_run WHERE id = $1`, staleID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID) })

	w := postBatchClaim(t, testWorkspaceID, []string{rt}, 5)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp batchClaimReceiptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// (1) The stale task must not be returned.
	for _, agentRun := range resp.Runs {
		if agentRun.ID == staleID {
			t.Fatalf("stale-plan task %s was dispatched by the batch path; want it repaired/omitted", staleID)
		}
	}
	// (2) The stale task must be cancelled.
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_run WHERE id = $1`, staleID).Scan(&status); err != nil {
		t.Fatalf("read stale status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("stale task status = %s, want cancelled", status)
	}
	// (3) The surviving comment was rebuilt into a fresh plan (a new task with
	// the survivor as its trigger).
	var rebuilt int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND trigger_comment_id = $2 AND id <> $3
	`, taskID, survivorID, staleID).Scan(&rebuilt); err != nil {
		t.Fatalf("count rebuilt: %v", err)
	}
	if rebuilt < 1 {
		t.Fatalf("expected the surviving comment rebuilt into a new trigger task, found %d", rebuilt)
	}
}
