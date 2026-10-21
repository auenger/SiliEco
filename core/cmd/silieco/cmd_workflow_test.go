package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowPreflight(t *testing.T) {
	directory := t.TempDir()
	contractPath := filepath.Join(directory, "contract.json")
	if err := os.WriteFile(contractPath, []byte(`{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["confirmed"],"properties":{"confirmed":{"type":"boolean","enum":[true]}}},"artifacts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, result string
		valid        bool
	}{
		{"valid", `{"data":{"confirmed":true},"artifacts":[]}`, true},
		{"invalid", `{"data":{"confirmed":false},"artifacts":[]}`, false},
		{"forged_acceptance", `{"data":{"confirmed":true},"artifacts":[],"review_status":"approved"}`, false},
		{"malformed", `{"data":`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			resultPath := filepath.Join(directory, test.name+".json")
			if err := os.WriteFile(resultPath, []byte(test.result), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := newWorkflowCmd()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs([]string{"preflight", "--contract", contractPath, "--result", resultPath})
			err := cmd.Execute()
			if (err == nil) != test.valid {
				t.Fatalf("unexpected command error: %v", err)
			}
			var report struct {
				Valid      bool   `json:"valid"`
				Scope      string `json:"scope"`
				Violations []any  `json:"violations"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Valid != test.valid || report.Scope != "local_preflight" || report.Violations == nil {
				t.Fatalf("invalid report: %s", output.String())
			}
		})
	}
}

func TestWorkflowResultCommands(t *testing.T) {
	const runID = "11111111-1111-4111-8111-111111111111"
	const executionID = "22222222-2222-4222-8222-222222222222"
	const artifactID = "33333333-3333-4333-8333-333333333333"
	directory := t.TempDir()
	resultPath := filepath.Join(directory, "result.json")
	if err := os.WriteFile(resultPath, []byte(`{"data":{},"artifacts":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "" {
			t.Error("missing authentication")
		}
		base := "/api/workflow-instances/" + runID + "/executions"
		switch r.URL.Path {
		case base:
			_, _ = w.Write([]byte(`[]`))
		case base + "/" + executionID + "/readiness":
			_, _ = w.Write([]byte(`{"scope":"readiness_snapshot","ready_for_review":false}`))
		case base + "/" + executionID + "/results":
			if r.Method != http.MethodPost {
				t.Error("expected POST")
			}
			var body struct {
				ExpectedRevision int32           `json:"expected_revision"`
				Submission       json.RawMessage `json:"submission"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.ExpectedRevision != 2 || string(body.Submission) != `{"data":{},"artifacts":[]}` {
				t.Errorf("wrong submission %+v", body)
			}
			_, _ = w.Write([]byte(`{"revision":3}`))
		case base + "/" + executionID + "/results/3":
			_, _ = w.Write([]byte(`{"revision":3,"artifacts":[]}`))
		case base + "/" + executionID + "/results/3/artifacts/" + artifactID:
			_, _ = w.Write([]byte("frozen bytes"))
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	execute := func(args ...string) error {
		cmd := newWorkflowCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	destination := filepath.Join(directory, "artifact.txt")
	for _, args := range [][]string{
		{"executions", runID},
		{"readiness", runID, executionID},
		{"submit", runID, executionID, "--expected-revision", "2", "--result", resultPath},
		{"result", runID, executionID, "--revision", "3"},
		{"artifact", runID, executionID, artifactID, "--revision", "3", "--file", destination},
	} {
		if err := execute(args...); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "frozen bytes" {
		t.Fatalf("download %q %v", content, err)
	}
	for _, args := range [][]string{
		{"executions", "../other"},
		{"submit", runID, executionID, "--result", resultPath},
		{"result", runID, executionID, "--revision", "0"},
		{"artifact", runID, executionID, artifactID, "--revision", "3", "--file", destination},
	} {
		if err := execute(args...); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
	if requests != 5 {
		t.Fatalf("unexpected requests: %d", requests)
	}
}

func TestWorkflowHumanReviewCommands(t *testing.T) {
	const runID = "11111111-1111-4111-8111-111111111111"
	const executionID = "22222222-2222-4222-8222-222222222222"
	basis := strings.Repeat("a", 64)
	requests := 0
	conflict := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") == "" {
			t.Error("missing authentication")
		}
		base := "/api/workflow-instances/" + runID + "/executions/" + executionID
		switch r.URL.Path {
		case base + "/review":
			if r.Method != http.MethodGet {
				t.Error("review must use GET")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"execution_id": executionID, "result_revision": 3, "basis_hash": basis, "issues": []any{}, "calls": []any{}})
		case base + "/decisions":
			if r.Method != http.MethodPost {
				t.Error("decision must use POST")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["expected_result_revision"] != float64(3) || body["expected_basis_hash"] != basis || body["outcome"] != "approved" || body["note"] != "Evidence checked\nReady" || len(body) != 4 {
				t.Errorf("wrong decision body: %+v", body)
			}
			if conflict {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"review evidence changed"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"decision-1","outcome":"approved"}`))
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	execute := func(args ...string) (string, error) {
		cmd := newWorkflowCmd()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(args)
		err := cmd.Execute()
		return output.String(), err
	}
	if output, err := execute("review", runID, executionID); err != nil || !strings.Contains(output, basis) {
		t.Fatalf("review: %s %v", output, err)
	}
	args := []string{"decision", runID, executionID, "--result-revision", "3", "--basis-hash", basis, "--outcome", "approved", "--note", "Evidence checked\nReady"}
	if output, err := execute(args...); err != nil || !strings.Contains(output, "decision-1") {
		t.Fatalf("decision: %s %v", output, err)
	}
	conflict = true
	if _, err := execute(args...); err == nil {
		t.Fatal("conflict should require another review")
	}
	if requests != 3 {
		t.Fatalf("CLI refreshed or retried stale approval: %d requests", requests)
	}
	for _, bad := range [][]string{
		{"decision", runID, executionID},
		{"decision", runID, executionID, "--result-revision", "0", "--basis-hash", basis, "--outcome", "approved"},
		{"decision", runID, executionID, "--result-revision", "3", "--basis-hash", strings.ToUpper(basis), "--outcome", "approved"},
		{"decision", runID, executionID, "--result-revision", "3", "--basis-hash", basis, "--outcome", "yes"},
		{"review", "../other", executionID},
	} {
		if _, err := execute(bad...); err == nil {
			t.Fatalf("invalid command accepted: %v", bad)
		}
	}
	if requests != 3 {
		t.Fatalf("invalid commands sent requests: %d", requests)
	}
}

func TestWorkflowInputCorrectionCommand(t *testing.T) {
	const runID = "11111111-1111-4111-8111-111111111111"
	const executionID = "22222222-2222-4222-8222-222222222222"
	requests := 0
	conflict := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/workflow-instances/"+runID+"/executions/"+executionID+"/inputs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Expected map[string]any `json:"expected_inputs"`
			Inputs   map[string]any `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Expected["goal"] != "old" || body.Inputs["goal"] != "new" {
			t.Errorf("wrong payload: %+v %v", body, err)
		}
		if conflict {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"input changed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	next := filepath.Join(dir, "new.json")
	for path, body := range map[string]string{old: `{"goal":"old"}`, next: `{"goal":"new"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	execute := func(args ...string) error {
		cmd := newWorkflowCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	args := []string{"correct-inputs", runID, executionID, "--expected-inputs", old, "--inputs", next}
	if err := execute(args...); err != nil {
		t.Fatal(err)
	}
	conflict = true
	if err := execute(args...); err == nil {
		t.Fatal("conflict hidden")
	}
	if requests != 2 {
		t.Fatalf("unexpected retry: %d", requests)
	}
	if err := execute("correct-inputs", runID, executionID, "--inputs", next); err == nil {
		t.Fatal("missing old inputs accepted")
	}
	for _, bad := range []string{`[]`, `null`, `{"goal":1,"goal":2}`, `{"goal":`} {
		if err := os.WriteFile(next, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if err := execute(args...); err == nil {
			t.Fatalf("invalid input accepted: %s", bad)
		}
	}
	if requests != 2 {
		t.Fatalf("invalid files sent requests: %d", requests)
	}
}

func TestWorkflowAgentReviewCommands(t *testing.T) {
	const runID = "11111111-1111-4111-8111-111111111111"
	const executionID = "22222222-2222-4222-8222-222222222222"
	const requestID = "33333333-3333-4333-8333-333333333333"
	basis := strings.Repeat("a", 64)
	requests := 0
	conflict := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer mat_review-test" || r.Header.Get("X-Agent-ID") != "reviewer" || r.Header.Get("X-Task-ID") != "review-call" {
			t.Error("lost reviewer authentication")
		}
		base := "/api/workflow-instances/" + runID + "/executions/" + executionID + "/reviews/" + requestID
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": requestID, "result_revision": 3, "basis_hash": basis})
		case r.Method == http.MethodPost && r.URL.Path == base+"/decisions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body) != 4 || body["expected_result_revision"] != float64(3) || body["expected_basis_hash"] != basis || body["outcome"] != "approved" || body["note"] != "Reviewed\nExact artifacts" {
				t.Errorf("wrong decision: %+v", body)
			}
			if conflict {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"review evidence changed"}`))
				return
			}
			_, _ = w.Write([]byte(`{"outcome":"approved"}`))
		default:
			t.Errorf("wrong route: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("SILIECO_TOKEN", "mat_review-test")
	t.Setenv("SILIECO_AGENT_ID", "reviewer")
	t.Setenv("SILIECO_TASK_ID", "review-call")
	execute := func(args ...string) (string, error) {
		cmd := newWorkflowCmd()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		cmd.SetArgs(args)
		err := cmd.Execute()
		return output.String(), err
	}
	if output, err := execute("agent-review", runID, executionID, requestID); err != nil || !strings.Contains(output, basis) {
		t.Fatalf("read: %s %v", output, err)
	}
	args := []string{"agent-decision", runID, executionID, requestID, "--result-revision", "3", "--basis-hash", basis, "--outcome", "approved", "--note", "Reviewed\nExact artifacts"}
	if output, err := execute(args...); err != nil || !strings.Contains(output, "approved") {
		t.Fatalf("decision: %s %v", output, err)
	}
	conflict = true
	if _, err := execute(args...); err == nil {
		t.Fatal("stale decision accepted")
	}
	for _, bad := range [][]string{
		{"agent-review", runID, executionID, "../other"},
		{"agent-decision", runID, executionID, requestID},
		{"agent-decision", runID, executionID, requestID, "--result-revision", "0", "--basis-hash", basis, "--outcome", "approved"},
		{"agent-decision", runID, executionID, requestID, "--result-revision", "3", "--basis-hash", strings.ToUpper(basis), "--outcome", "approved"},
		{"agent-decision", runID, executionID, requestID, "--result-revision", "3", "--basis-hash", basis, "--outcome", "yes"},
	} {
		if _, err := execute(bad...); err == nil {
			t.Fatalf("invalid command accepted: %v", bad)
		}
	}
	for _, identity := range [][2]string{{"", ""}, {"reviewer", ""}, {"", "review-call"}} {
		t.Setenv("SILIECO_AGENT_ID", identity[0])
		t.Setenv("SILIECO_TASK_ID", identity[1])
		if _, err := execute("agent-review", runID, executionID, requestID); err == nil {
			t.Fatal("missing call identity accepted")
		}
		if _, err := execute(args...); err == nil {
			t.Fatal("missing decision identity accepted")
		}
	}
	if requests != 3 {
		t.Fatalf("CLI retried a conflict or sent invalid input: %d", requests)
	}
}
