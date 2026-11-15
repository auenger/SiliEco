package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClient_ClaimTasks_PostsRuntimeSetAndParsesTasks verifies the machine-level
// batch claim (SILI-4257): the client POSTs to /api/daemon/runs/claim with the full
// runtime_id set + max_runs, and parses the {"runs":[...]} envelope, keeping
// each task's runtime_id so the daemon can route it locally.
func TestClient_ClaimRuns_PostsRuntimeSetAndParsesRuns(t *testing.T) {
	var gotPath string
	var gotBody struct {
		DaemonID   string   `json:"daemon_id"`
		RuntimeIDs []string `json:"runtime_ids"`
		MaxRuns    int      `json:"max_runs"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"runs":[
			{"id":"t1","runtime_id":"rt-a","agent":{"name":"a"}},
			{"id":"t2","runtime_id":"rt-b","agent":{"name":"b"}}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")

	agentRuns, err := c.ClaimRuns(context.Background(), "daemon-x", []string{"rt-a", "rt-b", "rt-c"}, 3)
	if err != nil {
		t.Fatalf("ClaimTasks: %v", err)
	}

	if gotPath != "/api/daemon/runs/claim" {
		t.Errorf("path = %q, want /api/daemon/runs/claim", gotPath)
	}
	if gotBody.DaemonID != "daemon-x" {
		t.Errorf("posted daemon_id = %q, want daemon-x", gotBody.DaemonID)
	}
	if len(gotBody.RuntimeIDs) != 3 || gotBody.RuntimeIDs[0] != "rt-a" || gotBody.MaxRuns != 3 {
		t.Errorf("posted body = %+v, want runtime_ids=[rt-a rt-b rt-c] max_runs=3", gotBody)
	}
	if len(agentRuns) != 2 {
		t.Fatalf("got %d tasks, want 2", len(agentRuns))
	}
	if agentRuns[0].ID != "t1" || agentRuns[0].RuntimeID != "rt-a" {
		t.Errorf("task[0] = %+v, want id=t1 runtime_id=rt-a", agentRuns[0])
	}
	if agentRuns[1].ID != "t2" || agentRuns[1].RuntimeID != "rt-b" {
		t.Errorf("task[1] = %+v, want id=t2 runtime_id=rt-b", agentRuns[1])
	}
}

// TestClient_ClaimTasks_EmptyResult confirms an empty batch (idle daemon) is
// returned as a nil/empty slice, not an error.
func TestClient_ClaimRuns_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"runs":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("tok")

	agentRuns, err := c.ClaimRuns(context.Background(), "daemon-x", []string{"rt-a"}, 1)
	if err != nil {
		t.Fatalf("ClaimTasks: %v", err)
	}
	if len(agentRuns) != 0 {
		t.Fatalf("got %d tasks, want 0", len(agentRuns))
	}
}
