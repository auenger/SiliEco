package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestLabelCRUD exercises label create/list/get/update/delete.
func TestLabelCRUD(t *testing.T) {
	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "bug",
		"color": "#ef4444",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created LabelResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Name != "bug" || created.Color != "#ef4444" {
		t.Fatalf("CreateLabel: unexpected payload: %+v", created)
	}
	labelID := created.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// Duplicate name → 409
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "BUG", // case-insensitive unique
		"color": "#000000",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("Duplicate CreateLabel: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid color → 400
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "enhancement",
		"color": "nope",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Invalid color: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels", nil)
	testHandler.ListLabels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabels: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Labels []LabelResponse `json:"labels"`
		Total  int             `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	if listResp.Total < 1 {
		t.Fatalf("ListLabels: expected >= 1 label, got %d", listResp.Total)
	}

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/labels/"+labelID, map[string]any{
		"name":  "Bug (P0)",
		"color": "#b91c1c",
	})
	req = withURLParam(req, "id", labelID)
	testHandler.UpdateLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated LabelResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "Bug (P0)" || updated.Color != "#b91c1c" {
		t.Fatalf("UpdateLabel: unexpected payload: %+v", updated)
	}
}

// TestTaskLabelAttachDetach exercises attach/detach + the task-scoped endpoints.
func TestTaskLabelAttachDetach(t *testing.T) {
	// Create task
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Task for label attach test",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	taskID := task.ID

	// Create label
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "feature",
		"color": "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// Attach
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+taskID+"/labels", map[string]any{
		"label_id": labelID,
	})
	req = withURLParam(req, "id", taskID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AttachLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Attach again (idempotent — ON CONFLICT DO NOTHING)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+taskID+"/labels", map[string]any{
		"label_id": labelID,
	})
	req = withURLParam(req, "id", taskID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AttachLabel (second): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List labels for task
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID+"/labels", nil)
	req = withURLParam(req, "id", taskID)
	testHandler.ListLabelsForTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabelsForTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var taskLabels struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&taskLabels)
	if len(taskLabels.Labels) != 1 {
		t.Fatalf("ListLabelsForTask: expected 1 label, got %d", len(taskLabels.Labels))
	}
	if taskLabels.Labels[0].ID != labelID {
		t.Fatalf("ListLabelsForTask: wrong label returned: %+v", taskLabels.Labels[0])
	}

	// Detach
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+taskID+"/labels/"+labelID, nil)
	req = withURLParams(req, "id", taskID, "labelId", labelID)
	testHandler.DetachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DetachLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm detached
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID+"/labels", nil)
	req = withURLParam(req, "id", taskID)
	testHandler.ListLabelsForTask(w, req)
	json.NewDecoder(w.Body).Decode(&taskLabels)
	if len(taskLabels.Labels) != 0 {
		t.Fatalf("after Detach: expected 0 labels, got %d", len(taskLabels.Labels))
	}
}

func TestTaskLabelRejectsNonTaskScope(t *testing.T) {
	withResourceLabelsFlag(t, testHandler, true)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Task rejects agent label",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"resource_type": "agent",
		"name":          "agent-only-" + uuid.NewString()[:8],
		"color":         "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var label LabelResponse
	if err := json.NewDecoder(w.Body).Decode(&label); err != nil {
		t.Fatalf("decode label: %v", err)
	}
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+label.ID, nil)
		req = withURLParam(req, "id", label.ID)
		testHandler.DeleteLabel(w, req)
	})

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+task.ID+"/labels", map[string]any{
		"label_id": label.ID,
	})
	req = withURLParam(req, "id", task.ID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("AttachLabel with agent label: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+task.ID+"/labels/"+label.ID, nil)
	req = withURLParams(req, "id", task.ID, "labelId", label.ID)
	testHandler.DetachLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DetachLabel with agent label: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestLabelNotFoundAcrossWorkspaces ensures GET with a foreign workspace
// header returns 404 — the query's `WHERE workspace_id = $2` does the work.
func TestLabelNotFoundAcrossWorkspaces(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "cross-ws-test",
		"color": "#a855f7",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// GET with a different workspace ID → 404
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetLabel cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateLabelCrossWorkspace — PUT with a foreign workspace header must not
// allow updating a label in another workspace (404 via pgx.ErrNoRows from the
// UPDATE ... WHERE id = $1 AND workspace_id = $2 clause).
func TestUpdateLabelCrossWorkspace(t *testing.T) {
	// Create in real workspace
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "cross-ws-update-test",
		"color": "#10b981",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d", w.Code)
	}
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// PUT with a foreign workspace ID → 404
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/labels/"+labelID, map[string]any{"name": "hacked"})
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	req = withURLParam(req, "id", labelID)
	testHandler.UpdateLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("UpdateLabel cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Sanity: the label wasn't renamed.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLabel after failed cross-workspace PUT: expected 200, got %d", w.Code)
	}
	var after LabelResponse
	json.NewDecoder(w.Body).Decode(&after)
	if after.Name != "cross-ws-update-test" {
		t.Fatalf("label name changed despite cross-workspace PUT: got %q", after.Name)
	}
}

// TestAttachLabelCrossWorkspaceLabel — an attach request whose label_id
// belongs to a different workspace must return 404, not silently no-op.
// Directly exercises the GetLabel workspace precheck and the SQL-layer
// defense-in-depth guard.
func TestAttachLabelCrossWorkspaceLabel(t *testing.T) {
	// Task in the test workspace
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "cross-ws-attach-task",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d", w.Code)
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)

	// Label in a second workspace — insert directly via the pool to avoid
	// the public API (which would require creating a full second workspace
	// fixture). The defense-in-depth is exactly that the handler refuses
	// even labels that exist *somewhere* but not in the current workspace.
	otherWorkspaceID := createOtherTestWorkspace(t)
	var otherLabelID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO task_label (workspace_id, name, color)
		VALUES ($1, 'foreign-label', '#000000')
		RETURNING id
	`, otherWorkspaceID).Scan(&otherLabelID)
	if err != nil {
		t.Fatalf("insert foreign label: %v", err)
	}

	// Try to attach the foreign label to the test-workspace task.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+task.ID+"/labels", map[string]any{
		"label_id": otherLabelID,
	})
	req = withURLParam(req, "id", task.ID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("AttachLabel cross-workspace label: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm nothing was attached.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+task.ID+"/labels", nil)
	req = withURLParam(req, "id", task.ID)
	testHandler.ListLabelsForTask(w, req)
	var list struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&list)
	if len(list.Labels) != 0 {
		t.Fatalf("expected 0 labels on task, got %d", len(list.Labels))
	}
}

// TestLabelNameTooLong — names longer than 32 chars must return 400.
func TestLabelNameTooLong(t *testing.T) {
	longName := strings.Repeat("a", 33)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  longName,
		"color": "#123456",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateLabel too-long name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly 32 chars is fine.
	okName := strings.Repeat("b", 32)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  okName,
		"color": "#123456",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel 32-char name: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created LabelResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+created.ID, nil)
		req = withURLParam(req, "id", created.ID)
		testHandler.DeleteLabel(w, req)
	})
}

func TestLabelNameRejectsControlCharacters(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		call func(*httptest.ResponseRecorder, *http.Request)
	}{
		{
			name: "create newline",
			body: map[string]any{"name": "bug\nurgent", "color": "#123456"},
			call: func(w *httptest.ResponseRecorder, req *http.Request) {
				testHandler.CreateLabel(w, req)
			},
		},
		{
			name: "create tab",
			body: map[string]any{"name": "bug\turgent", "color": "#123456"},
			call: func(w *httptest.ResponseRecorder, req *http.Request) {
				testHandler.CreateLabel(w, req)
			},
		},
		{
			name: "update control",
			body: map[string]any{"name": "bug\u0000urgent"},
			call: func(w *httptest.ResponseRecorder, req *http.Request) {
				req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
				testHandler.UpdateLabel(w, req)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/labels", tc.body)
			tc.call(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestLabelNameAllowsEmoji(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "🐛 bug",
		"color": "#123456",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel emoji name: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created LabelResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Name != "🐛 bug" {
		t.Fatalf("CreateLabel emoji name: got %q", created.Name)
	}
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+created.ID, nil)
		req = withURLParam(req, "id", created.ID)
		testHandler.DeleteLabel(w, req)
	})
}

func TestLabelResourceTypesHaveIndependentNamespaces(t *testing.T) {
	withResourceLabelsFlag(t, testHandler, true)
	name := "shared-scope-label-" + uuid.NewString()[:8]
	for _, resourceType := range []string{"task", "agent", "skill"} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/labels", map[string]any{
			"resource_type": resourceType,
			"name":          name,
			"description":   resourceType + " description",
			"color":         "#3b82f6",
		})
		testHandler.CreateLabel(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateLabel resource_type=%s: expected 201, got %d: %s", resourceType, w.Code, w.Body.String())
		}
		var created LabelResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode created label: %v", err)
		}
		if created.ResourceType != resourceType || created.Description != resourceType+" description" {
			t.Fatalf("unexpected scoped label: %+v", created)
		}
		t.Cleanup(func() {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/labels/"+created.ID, nil)
			req = withURLParam(req, "id", created.ID)
			testHandler.DeleteLabel(w, req)
		})
	}
}

func TestResourceLabelWritesRequireReleaseFlag(t *testing.T) {
	withResourceLabelsFlag(t, testHandler, false)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"resource_type": "agent",
		"name":          "blocked-resource-label-" + uuid.NewString()[:8],
		"color":         "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("CreateLabel without release flag: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteResourceLabelCleansAssignments(t *testing.T) {
	withResourceLabelsFlag(t, testHandler, true)
	agentID := createHandlerTestAgent(t, "Resource Label Cleanup", nil)
	skillID := insertHandlerTestSkill(t, "resource-label-cleanup", "# cleanup")

	create := func(resourceType string) LabelResponse {
		t.Helper()
		w := httptest.NewRecorder()
		testHandler.CreateLabel(w, newRequest("POST", "/api/labels", map[string]any{
			"resource_type": resourceType,
			"name":          resourceType + "-cleanup-" + uuid.NewString()[:8],
			"color":         "#3b82f6",
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s label: expected 201, got %d: %s", resourceType, w.Code, w.Body.String())
		}
		var label LabelResponse
		if err := json.NewDecoder(w.Body).Decode(&label); err != nil {
			t.Fatalf("decode %s label: %v", resourceType, err)
		}
		return label
	}

	agentLabel := create("agent")
	skillLabel := create("skill")
	for _, link := range []struct {
		query string
		id    string
		label string
	}{
		{`INSERT INTO agent_to_label (agent_id, label_id) VALUES ($1, $2)`, agentID, agentLabel.ID},
		{`INSERT INTO skill_to_label (skill_id, label_id) VALUES ($1, $2)`, skillID, skillLabel.ID},
	} {
		if _, err := testPool.Exec(context.Background(), link.query, link.id, link.label); err != nil {
			t.Fatalf("seed resource label assignment: %v", err)
		}
	}

	for _, labelID := range []string{agentLabel.ID, skillLabel.ID} {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("DELETE", "/api/labels/"+labelID, nil), "id", labelID)
		testHandler.DeleteLabel(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete resource label: expected 204, got %d: %s", w.Code, w.Body.String())
		}
	}

	for _, check := range []struct {
		query string
		id    string
	}{
		{`SELECT COUNT(*) FROM agent_to_label WHERE agent_id = $1`, agentID},
		{`SELECT COUNT(*) FROM skill_to_label WHERE skill_id = $1`, skillID},
	} {
		var count int
		if err := testPool.QueryRow(context.Background(), check.query, check.id).Scan(&count); err != nil {
			t.Fatalf("count remaining resource assignments: %v", err)
		}
		if count != 0 {
			t.Fatalf("resource label assignments remain after label deletion: %d", count)
		}
	}
}

func TestLabelRejectsUnknownResourceType(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"resource_type": "project",
		"name":          "invalid-scope",
		"color":         "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestColorCaseNormalization — input `#ABCDEF` must be stored as `#abcdef`
// so the case-insensitive uniqueness and downstream CSS rendering are
// consistent. Also accepts a bare `ABCDEF` (no leading #).
func TestColorCaseNormalization(t *testing.T) {
	cases := []struct {
		nameSuffix string
		input      string
		want       string
	}{
		{"upper", "#ABCDEF", "#abcdef"},
		{"mixed", "#AbCdEf", "#abcdef"},
		{"bare", "ABCDEF", "#abcdef"},
		{"lower", "#123abc", "#123abc"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		name := "color-norm-" + tc.nameSuffix // unique & case-independent
		req := newRequest("POST", "/api/labels", map[string]any{
			"name":  name,
			"color": tc.input,
		})
		testHandler.CreateLabel(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateLabel %q: expected 201, got %d: %s", tc.input, w.Code, w.Body.String())
		}
		var got LabelResponse
		json.NewDecoder(w.Body).Decode(&got)
		if got.Color != tc.want {
			t.Errorf("color normalization %q: got %q, want %q", tc.input, got.Color, tc.want)
		}
		t.Cleanup(func() {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/labels/"+got.ID, nil)
			req = withURLParam(req, "id", got.ID)
			testHandler.DeleteLabel(w, req)
		})
	}
}

// createOtherTestWorkspace inserts a second workspace + owner membership for
// cross-workspace tests. Returns the new workspace id; cleanup registered.
func createOtherTestWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var wsID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Other Handler Tests", handlerTestWorkspaceSlug+"-other", "temp second workspace", "OTH").Scan(&wsID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("add member to other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	return wsID
}
