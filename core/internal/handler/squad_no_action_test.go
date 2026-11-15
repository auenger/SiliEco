package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type runningSquadLeaderRunFixture struct {
	TaskID           string
	LeaderID         string
	RunID            string
	TriggerCommentID string
}

func newRunningSquadLeaderRunFixture(t *testing.T) runningSquadLeaderRunFixture {
	t.Helper()
	ctx := context.Background()

	fx := newSquadCommentTriggerFixture(t)
	taskID := uuidToString(fx.Task.ID)

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id FROM agent WHERE id = $1
	`, fx.LeaderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load leader runtime: %v", err)
	}

	var triggerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (task_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'LGTM', 'comment')
		RETURNING id
	`, taskID, testWorkspaceID, testUserID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("create trigger comment: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (
			agent_id, runtime_id, task_id, trigger_comment_id,
			status, priority, started_at
		)
		VALUES ($1, $2, $3, $4, 'running', 0, now())
		RETURNING id
	`, fx.LeaderID, runtimeID, taskID, triggerCommentID).Scan(&runID); err != nil {
		t.Fatalf("create running squad leader run: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID)
	})

	return runningSquadLeaderRunFixture{
		TaskID:           taskID,
		LeaderID:         fx.LeaderID,
		RunID:            runID,
		TriggerCommentID: triggerCommentID,
	}
}

func recordSquadLeaderEvaluationForRun(t *testing.T, fx runningSquadLeaderRunFixture, outcome string) {
	t.Helper()
	recordSquadLeaderEvaluationForRunWithHeader(t, fx, outcome, fx.RunID)
}

func recordSquadLeaderEvaluationForRunWithHeader(t *testing.T, fx runningSquadLeaderRunFixture, outcome, runIDHeader string) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+fx.TaskID+"/squad-evaluated", map[string]any{
		"outcome": outcome,
		"reason":  "test reason",
	})
	r = withURLParam(r, "id", fx.TaskID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Run-ID", runIDHeader)

	testHandler.RecordSquadLeaderEvaluation(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("RecordSquadLeaderEvaluation: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func completeRunningRun(t *testing.T, fx runningSquadLeaderRunFixture, output string) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newDaemonTokenRequest("POST", "/api/daemon/runs/"+fx.RunID+"/complete",
		map[string]any{"output": output},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runId", fx.RunID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteRun(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func countAgentCommentsForTask(t *testing.T, taskID, agentID string) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM comment
		WHERE task_id = $1 AND author_type = 'agent' AND author_id = $2
	`, taskID, agentID).Scan(&count); err != nil {
		t.Fatalf("count agent comments: %v", err)
	}
	return count
}

func TestCompleteRun_SquadLeaderNoActionDoesNotSynthesizeComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderRunFixture(t)
	recordSquadLeaderEvaluationForRun(t, fx, "no_action")

	completeRunningRun(t, fx, "No action needed. Exiting silently.")

	if got := countAgentCommentsForTask(t, fx.TaskID, fx.LeaderID); got != 0 {
		t.Fatalf("expected no squad leader comment after no_action completion, got %d", got)
	}
}

func TestCompleteRun_SquadLeaderNoActionCanonicalizesRunID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderRunFixture(t)
	recordSquadLeaderEvaluationForRunWithHeader(t, fx, "no_action", strings.ToUpper(fx.RunID))

	completeRunningRun(t, fx, "No action needed. Exiting silently.")

	if got := countAgentCommentsForTask(t, fx.TaskID, fx.LeaderID); got != 0 {
		t.Fatalf("expected no comment when no_action was recorded with uppercase task id header, got %d", got)
	}
}

func TestCompleteRun_SquadLeaderActionStillSynthesizesComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderRunFixture(t)
	recordSquadLeaderEvaluationForRun(t, fx, "action")

	completeRunningRun(t, fx, "Delegated the review.")

	if got := countAgentCommentsForTask(t, fx.TaskID, fx.LeaderID); got != 1 {
		t.Fatalf("expected action completion to synthesize one comment, got %d", got)
	}
}

func TestCreateComment_SquadLeaderNoActionRejectsComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	fx := newRunningSquadLeaderRunFixture(t)
	recordSquadLeaderEvaluationForRun(t, fx, "no_action")

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+fx.TaskID+"/comments", map[string]any{
		"content":   "No action needed.",
		"parent_id": fx.TriggerCommentID,
	})
	r = withURLParam(r, "id", fx.TaskID)
	r.Header.Set("X-Agent-ID", fx.LeaderID)
	r.Header.Set("X-Run-ID", fx.RunID)

	testHandler.CreateComment(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateComment: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if got := countAgentCommentsForTask(t, fx.TaskID, fx.LeaderID); got != 0 {
		t.Fatalf("expected rejected no_action comment not to be stored, got %d", got)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["error"] == "" {
		t.Fatalf("expected error message in response, got %v", body)
	}
}
