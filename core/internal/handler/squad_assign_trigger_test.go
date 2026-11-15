package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateTaskAssignedToSquadEnqueuesLeader verifies that creating an
// task with assignee_type=squad immediately enqueues a task for the squad
// leader (mirrors the agent-assignee parking-lot rule: skip backlog only).
func TestCreateTaskAssignedToSquadEnqueuesLeader(t *testing.T) {
	ctx := context.Background()

	// Look up the seeded test agent — it has a runtime, so it can lead a squad.
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	// Create a squad with that agent as leader.
	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Trigger Test Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID)

	// Create an task assigned to the squad.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Squad-assigned at creation",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
	}()

	// A task for the squad leader should now exist for this task.
	var runCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2
	`, created.ID, leaderID).Scan(&runCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if runCount == 0 {
		t.Fatalf("expected squad-leader task to be enqueued after squad-assigned create, got 0")
	}
}
