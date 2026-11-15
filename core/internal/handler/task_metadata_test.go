package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Round-trip: set primitives of each type, list, get them back, delete, confirm gone.
func TestTaskMetadataSetGetDelete(t *testing.T) {
	taskID := createMetadataTestTask(t, "Metadata round-trip")

	cases := []struct {
		key   string
		value string // raw JSON value
	}{
		{"pipeline_status", `"waiting"`},
		{"pr_number", `482`},
		{"is_blocked", `true`},
		{"is_done", `false`},
	}

	for _, c := range cases {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/"+c.key, json.RawMessage(`{"value":`+c.value+`}`))
		req = withURLParams(req, "id", taskID, "key", c.key)
		testHandler.SetTaskMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Set %s=%s: expected 200, got %d: %s", c.key, c.value, w.Code, w.Body.String())
		}
	}

	// List returns every key with the right value type.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/tasks/"+taskID+"/metadata", nil)
	req = withURLParam(req, "id", taskID)
	testHandler.ListTaskMetadata(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List metadata: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if got := resp.Metadata["pipeline_status"]; got != "waiting" {
		t.Errorf("pipeline_status: expected \"waiting\", got %T %v", got, got)
	}
	if got := resp.Metadata["pr_number"]; got != float64(482) {
		t.Errorf("pr_number: expected number 482, got %T %v", got, got)
	}
	if got := resp.Metadata["is_blocked"]; got != true {
		t.Errorf("is_blocked: expected true, got %T %v", got, got)
	}
	if got := resp.Metadata["is_done"]; got != false {
		t.Errorf("is_done: expected false, got %T %v", got, got)
	}

	// Delete a key — refresh confirms it is gone, others remain.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+taskID+"/metadata/pipeline_status", nil)
	req = withURLParams(req, "id", taskID, "key", "pipeline_status")
	testHandler.DeleteTaskMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Delete pipeline_status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID+"/metadata", nil)
	req = withURLParam(req, "id", taskID)
	testHandler.ListTaskMetadata(w, req)
	// Decode into a fresh struct — json.Decode into a non-nil map merges,
	// it does not replace, so reusing `resp` would keep deleted keys around.
	var afterDelete struct {
		Metadata map[string]any `json:"metadata"`
	}
	json.NewDecoder(w.Body).Decode(&afterDelete)
	if _, present := afterDelete.Metadata["pipeline_status"]; present {
		t.Errorf("after delete, pipeline_status should be gone; got %+v", afterDelete.Metadata)
	}
	if _, present := afterDelete.Metadata["pr_number"]; !present {
		t.Errorf("delete removed unrelated key; got %+v", afterDelete.Metadata)
	}
}

// Invalid keys / values / shapes are rejected with 400 — the regex, primitive,
// and "no null" rules must all hold.
func TestTaskMetadataValidation(t *testing.T) {
	taskID := createMetadataTestTask(t, "Metadata validation")

	bad := []struct {
		name    string
		key     string
		rawBody string
	}{
		{"key starts with digit", "1attempts", `{"value":"x"}`},
		{"key has space", "foo bar", `{"value":"x"}`},
		{"value is null", "k", `{"value":null}`},
		{"value is array", "k", `{"value":[1,2]}`},
		{"value is object", "k", `{"value":{"a":1}}`},
		{"empty body", "k", ``},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			// chi pulls the key from URL params (injected via withURLParams);
			// the raw URL needs to be a valid request line, so PathEscape any
			// chars (spaces, etc.) that would otherwise break httptest.NewRequest.
			req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/"+url.PathEscape(c.key), json.RawMessage(c.rawBody))
			req = withURLParams(req, "id", taskID, "key", c.key)
			testHandler.SetTaskMetadataKey(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// The 8KB DB CHECK kicks in past a few hundred KV pairs of large strings; we
// blow it deliberately with one giant value to confirm the handler surfaces
// a 400 (not a generic 500).
func TestTaskMetadataSizeLimit(t *testing.T) {
	taskID := createMetadataTestTask(t, "Metadata size limit")

	huge := strings.Repeat("a", 9000)
	body, _ := json.Marshal(map[string]any{"value": huge})
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/blob", body)
	req = withURLParams(req, "id", taskID, "key", "blob")
	testHandler.SetTaskMetadataKey(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from size CHECK, got %d: %s", w.Code, w.Body.String())
	}
}

// The 50-key cap is enforced in the handler with a clear 400.
func TestTaskMetadataKeyCountCap(t *testing.T) {
	taskID := createMetadataTestTask(t, "Metadata key count cap")

	for i := 0; i < maxTaskMetadataKeys; i++ {
		key := fmt.Sprintf("k_%d", i)
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/"+key, json.RawMessage(`{"value":"v"}`))
		req = withURLParams(req, "id", taskID, "key", key)
		testHandler.SetTaskMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("key #%d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/overflow", json.RawMessage(`{"value":"v"}`))
	req = withURLParams(req, "id", taskID, "key", "overflow")
	testHandler.SetTaskMetadataKey(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("overflow key: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Updating an existing key past the cap is still allowed — only new keys
	// are blocked.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+taskID+"/metadata/k_0", json.RawMessage(`{"value":"v2"}`))
	req = withURLParams(req, "id", taskID, "key", "k_0")
	testHandler.SetTaskMetadataKey(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update existing at cap: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ListTasks with `metadata` query param does JSONB containment filtering and
// returns only matching tasks — the killer use case for autopilot.
func TestListTasksMetadataFilter(t *testing.T) {
	waitingID := createMetadataTestTask(t, "Waiting task")
	doneID := createMetadataTestTask(t, "Done task")

	for taskID, status := range map[string]string{waitingID: "waiting_review", doneID: "deployed"} {
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/tasks/"+taskID+"/metadata/pipeline_status",
			json.RawMessage(`{"value":"`+status+`"}`))
		req = withURLParams(req, "id", taskID, "key", "pipeline_status")
		testHandler.SetTaskMetadataKey(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("seed %s: %d %s", taskID, w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := newRequest("GET", `/api/tasks?metadata={"pipeline_status":"waiting_review"}`, nil)
	testHandler.ListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List with filter: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Tasks []TaskResponse `json:"tasks"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)

	foundWaiting := false
	for _, iss := range listResp.Tasks {
		if iss.ID == doneID {
			t.Errorf("filter leaked: deployed task %s appeared in waiting_review result set", doneID)
		}
		if iss.ID == waitingID {
			foundWaiting = true
			if got, _ := iss.Metadata["pipeline_status"].(string); got != "waiting_review" {
				t.Errorf("waiting task: pipeline_status not surfaced; got %v", iss.Metadata)
			}
		}
	}
	if !foundWaiting {
		t.Errorf("waiting task %s missing from filter result; got %d tasks", waitingID, len(listResp.Tasks))
	}

	// Malformed filter → 400.
	w = httptest.NewRecorder()
	req = newRequest("GET", `/api/tasks?metadata={not-json}`, nil)
	testHandler.ListTasks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed metadata: expected 400, got %d", w.Code)
	}
}

// New tasks default to an empty metadata object — never null — so frontend
// reads like `task.metadata[key]` never NPE.
func TestNewTaskDefaultsToEmptyMetadata(t *testing.T) {
	taskID := createMetadataTestTask(t, "Default empty metadata")

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/tasks/"+taskID, nil)
	req = withURLParam(req, "id", taskID)
	testHandler.GetTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTask: %d %s", w.Code, w.Body.String())
	}
	var got TaskResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.Metadata == nil {
		t.Fatalf("Metadata is nil on a fresh task; expected empty object")
	}
	if len(got.Metadata) != 0 {
		t.Fatalf("Metadata: expected empty, got %v", got.Metadata)
	}
}

func createMetadataTestTask(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createMetadataTestTask: %d %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return task.ID
}
