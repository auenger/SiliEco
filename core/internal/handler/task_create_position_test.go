package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestCreateTaskPositionTopOfColumn verifies that a newly created task is
// placed above all existing tasks in the same status column (manual sort order).
//
// Before the fix, new tasks were always assigned position=0. After drag-reorder
// activity, existing tasks accumulate negative positions at the top of the
// column, so a fresh task at 0 would land in the middle of a long list.
//
// The fix queries MIN(position) for the workspace+status pair and assigns
// newPosition = minPos - 1, so the new ticket always appears first.
func TestCreateTaskPositionTopOfColumn(t *testing.T) {
	// Create two tasks via the API. The first lands at COALESCE(MIN,0)-1 = -1,
	// the second at -2, and so on — each successive task ends up above the
	// previous one, which is exactly the desired behavior.
	createTaskAndGetPosition := func(t *testing.T, title string) (string, float64) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
			"title":    title,
			"status":   "todo",
			"priority": "low",
		})
		testHandler.CreateTask(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateTask %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var task TaskResponse
		json.NewDecoder(w.Body).Decode(&task)
		return task.ID, task.Position
	}

	id1, pos1 := createTaskAndGetPosition(t, "position-test first task")
	t.Cleanup(func() { deleteTestTask(t, id1) })

	id2, pos2 := createTaskAndGetPosition(t, "position-test second task")
	t.Cleanup(func() { deleteTestTask(t, id2) })

	id3, pos3 := createTaskAndGetPosition(t, "position-test third task")
	t.Cleanup(func() { deleteTestTask(t, id3) })

	// Each new task must have a strictly lower position than the previous one,
	// ensuring it sorts to the top of the column in manual order.
	if pos2 >= pos1 {
		t.Errorf("second task position (%v) should be less than first (%v)", pos2, pos1)
	}
	if pos3 >= pos2 {
		t.Errorf("third task position (%v) should be less than second (%v)", pos3, pos2)
	}
}

// TestCreateTaskPositionBelowExplicitMinimum verifies the fix against a
// realistic drag-reordered column: after manually setting a low position
// directly in the DB (simulating drag-and-drop), a new task created via the
// API should land below the explicit minimum, not at 0.
func TestCreateTaskPositionBelowExplicitMinimum(t *testing.T) {
	// Create a seed task via the API.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "position-seed task",
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var seed TaskResponse
	json.NewDecoder(w.Body).Decode(&seed)
	t.Cleanup(func() { deleteTestTask(t, seed.ID) })

	// Simulate drag-and-drop: overwrite the seed's position to a large negative
	// value (-9999), as if the user dragged it to the very top of a long list.
	const simulatedMinPos = -9999.0
	if _, err := testPool.Exec(t.Context(),
		`UPDATE task SET position = $1 WHERE id = $2`,
		simulatedMinPos, seed.ID,
	); err != nil {
		t.Fatalf("failed to set explicit position: %v", err)
	}

	// Now create a new task. It must land below -9999, not at 0.
	w2 := httptest.NewRecorder()
	req2 := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "position-new task",
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateTask(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("new CreateTask: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var newTask TaskResponse
	json.NewDecoder(w2.Body).Decode(&newTask)
	t.Cleanup(func() { deleteTestTask(t, newTask.ID) })

	if newTask.Position >= simulatedMinPos {
		t.Errorf("new task position (%v) should be less than simulated min (%v); got position 0 (unfixed behavior)?",
			newTask.Position, simulatedMinPos)
	}
}

func TestAutopilotCreateTaskPositionBelowCurrentMinimum(t *testing.T) {
	ctx := context.Background()
	seedTitle := fmt.Sprintf("position-autopilot seed %d", time.Now().UnixNano())
	autopilotTaskTitle := fmt.Sprintf("position-autopilot task %d", time.Now().UnixNano())

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    seedTitle,
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var seed TaskResponse
	json.NewDecoder(w.Body).Decode(&seed)
	t.Cleanup(func() { deleteTestTask(t, seed.ID) })

	const simulatedMinPos = -9999.0
	if _, err := testPool.Exec(ctx,
		`UPDATE task SET position = $1 WHERE id = $2`,
		simulatedMinPos, seed.ID,
	); err != nil {
		t.Fatalf("failed to set explicit position: %v", err)
	}

	var minBefore float64
	if err := testPool.QueryRow(ctx,
		`SELECT MIN(position) FROM task WHERE workspace_id = $1 AND status = 'todo'`,
		testWorkspaceID,
	).Scan(&minBefore); err != nil {
		t.Fatalf("load min position: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Position autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": autopilotTaskTitle,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilot.ID) })

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilot.ID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.TaskID.Valid {
		t.Fatalf("dispatch run = %+v, want linked task", run)
	}
	taskID := uuidToString(run.TaskID)
	t.Cleanup(func() { deleteTestTask(t, taskID) })

	var createdPos float64
	if err := testPool.QueryRow(ctx, `SELECT position FROM task WHERE id = $1`, taskID).Scan(&createdPos); err != nil {
		t.Fatalf("load autopilot-created task position: %v", err)
	}
	if createdPos >= minBefore {
		t.Errorf("autopilot-created task position (%v) should be less than current min (%v); fixed position 0 would sort in the middle",
			createdPos, minBefore)
	}
}
