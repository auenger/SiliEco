package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBatchUpdateNoMutationReturnsZero — regression for #1660.
//
// When the request payload has valid task_ids but the "updates" field
// is empty, missing, or doesn't decode any known mutation field, the
// handler used to walk every task, run a no-op UPDATE, and increment
// `updated` for each one — returning {"updated": N} despite changing
// nothing. Reporters saw 200 + a positive count and assumed the call
// worked, then chased a phantom persistence bug.
//
// The fix is "tell the truth": when no mutation field is present, return
// {"updated": 0} immediately so the count matches reality.
func TestBatchUpdateNoMutationReturnsZero(t *testing.T) {
	// Two fresh tasks so we can also assert no fields actually changed.
	a := createTestTask(t, "BU-no-mut A", "todo", "low")
	b := createTestTask(t, "BU-no-mut B", "todo", "low")
	t.Cleanup(func() { deleteTestTask(t, a) })
	t.Cleanup(func() { deleteTestTask(t, b) })

	cases := []struct {
		desc string
		body map[string]any
	}{
		{
			desc: "updates_missing",
			// Most common reporter pattern: status at top level.
			body: map[string]any{"task_ids": []string{a, b}, "status": "in_progress"},
		},
		{
			desc: "updates_empty_object",
			body: map[string]any{"task_ids": []string{a, b}, "updates": map[string]any{}},
		},
		{
			desc: "updates_misnamed",
			// Singular "update" instead of plural "updates".
			body: map[string]any{"task_ids": []string{a, b}, "update": map[string]any{"status": "done"}},
		},
		{
			desc: "updates_unknown_field_only",
			// Payload IS nested correctly, but every key inside `updates` is
			// unknown to the handler — same class of caller mistake as the
			// shapes above. hasMutation must stay false; behavior is already
			// correct, this case locks it in against future regressions.
			body: map[string]any{"task_ids": []string{a, b}, "updates": map[string]any{"foo": "bar"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/tasks/batch-update", tc.body)
			testHandler.BatchUpdateTasks(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Updated int `json:"updated"`
			}
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Updated != 0 {
				t.Errorf("expected updated=0 when no mutation field present, got %d", resp.Updated)
			}

			// Belt and braces: confirm the tasks weren't touched.
			for _, id := range []string{a, b} {
				gw := httptest.NewRecorder()
				gr := newRequest("GET", "/api/tasks/"+id, nil)
				gr = withURLParam(gr, "id", id)
				testHandler.GetTask(gw, gr)
				var got TaskResponse
				json.NewDecoder(gw.Body).Decode(&got)
				if got.Status != "todo" {
					t.Errorf("task %s: status changed to %q despite no-mutation request", id, got.Status)
				}
			}
		})
	}
}

// TestBatchUpdateValidUpdatesPersistAndCount — positive case to lock in
// happy-path behavior alongside the regression test above.
func TestBatchUpdateValidUpdatesPersistAndCount(t *testing.T) {
	a := createTestTask(t, "BU-ok A", "todo", "low")
	b := createTestTask(t, "BU-ok B", "todo", "low")
	t.Cleanup(func() { deleteTestTask(t, a) })
	t.Cleanup(func() { deleteTestTask(t, b) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": []string{a, b},
		"updates":  map[string]any{"status": "in_progress"},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 2 {
		t.Errorf("expected updated=2, got %d", resp.Updated)
	}
	for _, id := range []string{a, b} {
		gw := httptest.NewRecorder()
		gr := newRequest("GET", "/api/tasks/"+id, nil)
		gr = withURLParam(gr, "id", id)
		testHandler.GetTask(gw, gr)
		var got TaskResponse
		json.NewDecoder(gw.Body).Decode(&got)
		if got.Status != "in_progress" {
			t.Errorf("task %s: expected status=in_progress, got %q", id, got.Status)
		}
	}
}

// TestBatchUpdateStageOnly — regression for the stage barrier feature: a
// batch update whose only field is `stage` must count as a mutation (hasMutation
// includes "stage") and actually persist, not silently return {"updated": 0}.
func TestBatchUpdateStageOnly(t *testing.T) {
	a := createTestTask(t, "BU-stage A", "todo", "low")
	t.Cleanup(func() { deleteTestTask(t, a) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": []string{a},
		"updates":  map[string]any{"stage": 2},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 1 {
		t.Fatalf("expected updated=1 for a stage-only batch update, got %d", resp.Updated)
	}

	gw := httptest.NewRecorder()
	gr := newRequest("GET", "/api/tasks/"+a, nil)
	gr = withURLParam(gr, "id", a)
	testHandler.GetTask(gw, gr)
	var got TaskResponse
	json.NewDecoder(gw.Body).Decode(&got)
	if got.Stage == nil || *got.Stage != 2 {
		t.Errorf("expected stage=2 to persist, got %v", got.Stage)
	}
}

// createTestTask is a small helper to keep the table-driven cases clean.
// Returns the new task's id; caller is responsible for cleanup.
func createTestTask(t *testing.T, title, status, priority string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   status,
		"priority": priority,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	return task.ID
}

func deleteTestTask(t *testing.T, id string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/tasks/"+id, nil)
	req = withURLParam(req, "id", id)
	testHandler.DeleteTask(w, req)
}

// --- SILI-4155: batch cross-stage child-done aggregation ---
//
// A single batch that finishes sub-tasks spanning multiple stages must
// evaluate the parent stage barrier ONCE against the batch's final committed
// state, not per-child mid-batch. The mid-batch behaviour fired one comment per
// intermediate stage, pinned the parent assignee's wake to a stale "advance the
// next stage" instruction, and produced order-dependent output. These tests
// lock in the aggregated behaviour: at most one accurate comment + one wake per
// parent, identical regardless of task_ids order.

// stagedBatchFixture is a parent (agent-assigned, in_progress) with two ordered
// stages of two in_progress children each, so a single batch that finishes all
// four exercises the cross-stage barrier aggregation.
type stagedBatchFixture struct {
	parent  TaskResponse
	agentID string
	stage1  []TaskResponse
	stage2  []TaskResponse
}

func newStagedBatchFixture(t *testing.T) stagedBatchFixture {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}

	pw := httptest.NewRecorder()
	preq := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "batch-stage parent " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateTask(pw, preq)
	if pw.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", pw.Code, pw.Body.String())
	}
	var parent TaskResponse
	json.NewDecoder(pw.Body).Decode(&parent)

	// Assign the parent to the ready test agent via direct SQL so the child-done
	// wake actually enqueues a task we can pin to the final comment — without the
	// assignment trigger queuing an unrelated task at setup.
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("locate test agent: %v", err)
	}
	setTaskAssigneeDirect(t, parent.ID, "agent", agentID)

	mkChild := func(stage int32) TaskResponse {
		cw := httptest.NewRecorder()
		creq := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
			"title":          "batch-stage child " + time.Now().Format(time.RFC3339Nano),
			"status":         "in_progress",
			"parent_task_id": parent.ID,
		})
		testHandler.CreateTask(cw, creq)
		if cw.Code != http.StatusCreated {
			t.Fatalf("create child: expected 201, got %d: %s", cw.Code, cw.Body.String())
		}
		var child TaskResponse
		json.NewDecoder(cw.Body).Decode(&child)
		// Set the stage directly — the barrier logic only reads task.stage.
		if _, err := testPool.Exec(context.Background(),
			`UPDATE task SET stage = $2 WHERE id = $1`, child.ID, stage); err != nil {
			t.Fatalf("set child stage: %v", err)
		}
		return child
	}

	fx := stagedBatchFixture{parent: parent, agentID: agentID}
	fx.stage1 = []TaskResponse{mkChild(1), mkChild(1)}
	fx.stage2 = []TaskResponse{mkChild(2), mkChild(2)}

	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, parent.ID)
		for _, c := range append(append([]TaskResponse{}, fx.stage1...), fx.stage2...) {
			testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, c.ID)
		}
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, parent.ID)
	})

	return fx
}

// batchSetStatus drives BatchUpdateTasks over the given ids, in order.
func batchSetStatus(t *testing.T, ids []string, status string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks/batch-update", map[string]any{
		"task_ids": ids,
		"updates":  map[string]any{"status": status},
	})
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateTasks status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// systemCommentIDOn returns the id of the single (latest) system comment on taskID.
func systemCommentIDOn(t *testing.T, taskID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id::text FROM comment
		   WHERE task_id = $1 AND author_type = 'system'
		   ORDER BY created_at DESC LIMIT 1`,
		taskID,
	).Scan(&id); err != nil {
		t.Fatalf("read system comment id: %v", err)
	}
	return id
}

// triggerCommentIDForAgentTask returns the trigger_comment_id of the single
// pending task on taskID for agentID.
func triggerCommentIDForAgentRun(t *testing.T, taskID, agentID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`SELECT trigger_comment_id::text FROM agent_run
		   WHERE task_id = $1 AND agent_id = $2
		     AND status IN ('queued','dispatched','running')
		   ORDER BY created_at DESC LIMIT 1`,
		taskID, agentID,
	).Scan(&id); err != nil {
		t.Fatalf("read task trigger_comment_id: %v", err)
	}
	return id
}

// TestBatchChildDoneCrossStage_OneComment is the SILI-4155 core. A single batch
// that finishes children across two stages must produce exactly ONE accurate
// system comment on the parent — announcing the highest stage closed by the
// final state, never a stale "Stage 2 is next" — and the parent assignee's
// single wake must be pinned to that final comment, regardless of id order.
func TestBatchChildDoneCrossStage_OneComment(t *testing.T) {
	assertFinal := func(t *testing.T, parentID, agentID string) {
		t.Helper()
		if got := countSystemCommentsOn(t, parentID); got != 1 {
			t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
		}
		content, _, _, _ := systemCommentOn(t, parentID)
		if !strings.Contains(content, "Stage 2 of this task is complete") {
			t.Errorf("expected the comment to announce the top closed stage (Stage 2), got: %s", content)
		}
		if !strings.Contains(content, "Stage 1: 2/2 done; Stage 2: 2/2 done") {
			t.Errorf("expected the final-state stage summary, got: %s", content)
		}
		// The bug: a mid-batch snapshot told the parent to advance a stage this
		// same batch had already finished.
		if strings.Contains(content, "is next") || strings.Contains(content, "(next)") {
			t.Errorf("comment must not carry a stale next-stage instruction, got: %s", content)
		}
		// Exactly one wake, pinned to the final comment.
		if got := countPendingRunsForAgent(t, parentID, agentID); got != 1 {
			t.Fatalf("expected exactly 1 pending parent task, got %d", got)
		}
		if trig, want := triggerCommentIDForAgentRun(t, parentID, agentID), systemCommentIDOn(t, parentID); trig != want {
			t.Errorf("parent wake pinned to %s, want the final comment %s", trig, want)
		}
	}

	t.Run("forward order [stage1, stage2]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})

	t.Run("reverse order [stage2, stage1]", func(t *testing.T) {
		fx := newStagedBatchFixture(t)
		batchSetStatus(t, []string{fx.stage2[0].ID, fx.stage2[1].ID, fx.stage1[0].ID, fx.stage1[1].ID}, "done")
		assertFinal(t, fx.parent.ID, fx.agentID)
	})
}

// TestBatchChildDoneCrossStage_Cancelled — cancelling every stage in one batch
// is terminal too and must behave identically: one accurate final comment, no
// stale advance instruction.
func TestBatchChildDoneCrossStage_Cancelled(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID, fx.stage2[0].ID, fx.stage2[1].ID}, "cancelled")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 2 of this task is complete") {
		t.Errorf("expected Stage 2 completion announcement, got: %s", content)
	}
	if strings.Contains(content, "is next") || strings.Contains(content, "(next)") {
		t.Errorf("comment must not carry a stale next-stage instruction, got: %s", content)
	}
}

// TestBatchChildDoneClosesLowerStageOnly — when a batch finishes only the lower
// stage (a later stage still has open children), the parent must be told Stage 1
// is complete AND accurately pointed at Stage 2 as next. Guards against
// over-suppressing the legitimate advance instruction.
func TestBatchChildDoneClosesLowerStageOnly(t *testing.T) {
	fx := newStagedBatchFixture(t)
	batchSetStatus(t, []string{fx.stage1[0].ID, fx.stage1[1].ID}, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, _, _, _ := systemCommentOn(t, fx.parent.ID)
	if !strings.Contains(content, "Stage 1 of this task is complete") {
		t.Errorf("expected Stage 1 completion announcement, got: %s", content)
	}
	if !strings.Contains(content, "Stage 2: 0/2 done (next)") {
		t.Errorf("expected accurate next-stage progress, got: %s", content)
	}
	if !strings.Contains(content, "Stage 2 is next") {
		t.Errorf("expected the advance-to-next-stage instruction, got: %s", content)
	}
}
