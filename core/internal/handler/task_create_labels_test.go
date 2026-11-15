package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// createTestTaskLabel creates an task-scoped label in the test workspace via
// the public handler and registers cleanup. It returns the new label id.
func createTestTaskLabel(t *testing.T, name string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  name,
		"color": "#ef4444",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var created LabelResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_label WHERE id = $1`, created.ID)
	})
	return created.ID
}

func countTaskLabel(t *testing.T, taskID, labelID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task_to_label WHERE task_id = $1 AND label_id = $2`,
		taskID, labelID,
	).Scan(&n); err != nil {
		t.Fatalf("count task_to_label: %v", err)
	}
	return n
}

func countTasksWithTitle(t *testing.T, title string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, title,
	).Scan(&n); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return n
}

// TestCreateTaskAttachesLabelsAtomically verifies that labels passed in the
// create request are written in the same transaction as the task, so a
// created task already carries its labels without a second round-trip.
func TestCreateTaskAttachesLabelsAtomically(t *testing.T) {
	labelA := createTestTaskLabel(t, "cl-a-"+uuid.NewString()[:8])
	labelB := createTestTaskLabel(t, "cl-b-"+uuid.NewString()[:8])

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "create-labels attaches",
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{labelA, labelB},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	t.Cleanup(func() { deleteTestTask(t, task.ID) })

	if got := countTaskLabel(t, task.ID, labelA); got != 1 {
		t.Errorf("label A: expected 1 attachment, got %d", got)
	}
	if got := countTaskLabel(t, task.ID, labelB); got != 1 {
		t.Errorf("label B: expected 1 attachment, got %d", got)
	}

	// The create response must echo the authoritative labels so every online
	// client (not just the creator) renders the new task already labeled and
	// a newer client can tell the backend understood label_ids.
	if task.Labels == nil {
		t.Fatalf("create response must include a labels field")
	}
	got := map[string]bool{}
	for _, l := range *task.Labels {
		got[l.ID] = true
	}
	if !got[labelA] || !got[labelB] {
		t.Errorf("response labels %v missing labelA=%s or labelB=%s", got, labelA, labelB)
	}
}

// TestCreateTaskResponseAlwaysIncludesLabelsField verifies that even when no
// labels are requested the create response carries an explicit (empty) labels
// list — the signal a newer client uses to know the backend handled labels and
// skip its legacy per-label attach fallback.
func TestCreateTaskResponseAlwaysIncludesLabelsField(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "create-labels no-label response",
		"status":   "todo",
		"priority": "low",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	t.Cleanup(func() { deleteTestTask(t, task.ID) })

	if task.Labels == nil {
		t.Fatalf("create response must include a labels field even when empty")
	}
	if len(*task.Labels) != 0 {
		t.Errorf("expected empty labels, got %d", len(*task.Labels))
	}
}

// TestCreateTaskEventCarriesLabels verifies that the task:created websocket
// event carries the attached labels, so online members other than the creator
// render the new task already labeled instead of blank until a refetch (the
// old flow relied on a separate task_labels:changed broadcast this PR removed).
func TestCreateTaskEventCarriesLabels(t *testing.T) {
	label := createTestTaskLabel(t, "cl-evt-"+uuid.NewString()[:8])

	gotEvent := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventTaskCreated, func(e events.Event) {
		select {
		case gotEvent <- e:
		default:
		}
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "create-labels event carries",
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{label},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	t.Cleanup(func() { deleteTestTask(t, task.ID) })

	select {
	case ev := <-gotEvent:
		pm, ok := ev.Payload.(map[string]any)
		if !ok {
			t.Fatalf("task:created payload has unexpected type %T", ev.Payload)
		}
		resp, ok := pm["task"].(TaskResponse)
		if !ok {
			t.Fatalf("task:created payload task has unexpected type %T", pm["task"])
		}
		if resp.Labels == nil {
			t.Fatalf("task:created payload must include a labels field")
		}
		found := false
		for _, l := range *resp.Labels {
			if l.ID == label {
				found = true
			}
		}
		if !found {
			t.Errorf("task:created labels %+v missing %s", *resp.Labels, label)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive task:created event")
	}
}

// TestCreateTaskDedupesDuplicateLabelIDs verifies that a repeated label id in
// the request attaches exactly once (ON CONFLICT DO NOTHING) and still 201s.
func TestCreateTaskDedupesDuplicateLabelIDs(t *testing.T) {
	label := createTestTaskLabel(t, "cl-dup-"+uuid.NewString()[:8])

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":     "create-labels dedupe",
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{label, label},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	t.Cleanup(func() { deleteTestTask(t, task.ID) })

	if got := countTaskLabel(t, task.ID, label); got != 1 {
		t.Errorf("expected label attached exactly once, got %d", got)
	}
}

// TestCreateTaskRejectsUnknownLabelWithoutCreating verifies that an unknown
// label id fails the whole create with 400 and leaves no task behind — the
// atomicity guarantee the old post-create attach flow could not offer.
func TestCreateTaskRejectsUnknownLabelWithoutCreating(t *testing.T) {
	const title = "create-labels unknown rejected"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":     title,
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{uuid.NewString()},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTasksWithTitle(t, title); got != 0 {
		t.Errorf("expected no task created on invalid label, found %d", got)
	}
}

// TestCreateTaskRejectsNonTaskScopedLabel verifies that a label scoped to a
// non-task resource (e.g. agent) cannot be attached to an task at create,
// mirroring the resource_type guard baked into AttachLabelToTask.
func TestCreateTaskRejectsNonTaskScopedLabel(t *testing.T) {
	var agentLabelID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO task_label (workspace_id, resource_type, name, color)
		 VALUES ($1, 'agent', $2, '#000000') RETURNING id`,
		testWorkspaceID, "create-labels agent "+uuid.NewString(),
	).Scan(&agentLabelID); err != nil {
		t.Fatalf("insert agent label: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task_label WHERE id = $1`, agentLabelID)
	})

	const title = "create-labels wrong scope rejected"
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":     title,
		"status":    "todo",
		"priority":  "low",
		"label_ids": []string{agentLabelID},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := countTasksWithTitle(t, title); got != 0 {
		t.Errorf("expected no task created on wrong-scope label, found %d", got)
	}
}
