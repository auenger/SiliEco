package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/silieco-ai/silieco/core/internal/cli"
)

// Tests for `silieco task metadata list` 404-degradation behavior, plus
// regression coverage that get / set / delete keep real error semantics so
// we don't lose signal when the user actually depends on the metadata
// endpoint working.
//
// Background: GitHub task silieco-ai/silieco#3711 — on self-hosted
// backends that pre-date the per-task metadata route, agent runtime
// bootstrap calls `silieco task metadata list <task> --output json`
// best-effort and any non-zero exit was being escalated by the Hermes
// provider into a failed agent run. The fix is to treat a 404 from
// /api/tasks/{id}/metadata as "this server has no metadata yet" and
// emit `{}` with exit 0 — but only for `list`, since get/set/delete on
// a missing endpoint really are operational failures the caller asked
// for.

const testTaskUUID = "11111111-1111-1111-1111-111111111111"

func newTaskMetadataListTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "list"}
	c.Flags().String("output", "json", "")
	return c
}

func newTaskMetadataGetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "get"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

func newTaskMetadataSetTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "set"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	c.Flags().String("value", "", "")
	c.Flags().String("type", "", "")
	return c
}

func newTaskMetadataDeleteTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "delete"}
	c.Flags().String("output", "json", "")
	c.Flags().String("key", "", "")
	return c
}

// captureStdout used in this file is the (string, error) helper defined
// in cmd_skill_test.go.

// metadataTestServer wires a minimal fake backend that answers the
// resolveTaskRef GET on /api/tasks/<id> and forwards every metadata
// request to the supplied handler. It returns the captured request paths
// in order so callers can assert routing.
func metadataTestServer(t *testing.T, metadataHandler http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/tasks/"+testTaskUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         testTaskUUID,
				"identifier": "SILI-1",
				"title":      "test task",
			})
		case strings.HasPrefix(r.URL.Path, "/api/tasks/"+testTaskUUID+"/metadata"):
			metadataHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SILIECO_SERVER_URL", srv.URL)
	t.Setenv("SILIECO_WORKSPACE_ID", "ws-1")
	t.Setenv("SILIECO_TOKEN", "test-token")
	return srv, &paths
}

func TestRunTaskMetadataListDegradesOn404JSON(t *testing.T) {
	var hits int32
	_, paths := metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	cmd := newTaskMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runTaskMetadataList(cmd, []string{testTaskUUID})
	})
	if runErr != nil {
		t.Fatalf("runTaskMetadataList returned error on 404, want nil: %v", runErr)
	}
	if got := strings.TrimSpace(out); got != "{}" {
		t.Fatalf("stdout = %q, want %q (empty JSON object on 404 degradation)", got, "{}")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("metadata endpoint hits = %d, want 1; routing: %v", got, *paths)
	}
}

func TestRunTaskMetadataListDegradesOn404Table(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such route", http.StatusNotFound)
	})

	cmd := newTaskMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "table")

	out, runErr := captureStdout(t, func() error {
		return runTaskMetadataList(cmd, []string{testTaskUUID})
	})
	if runErr != nil {
		t.Fatalf("runTaskMetadataList returned error on 404 table mode: %v", runErr)
	}
	// Table mode prints headers even with zero rows; the important
	// invariant is just that exit is clean and the row table doesn't
	// surface a stack trace or error blob.
	if !strings.Contains(out, "KEY") {
		t.Fatalf("table output missing KEY header, got %q", out)
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(out, "404") {
		t.Fatalf("table output unexpectedly leaked error text: %q", out)
	}
}

func TestRunTaskMetadataListSuccessReturnsServerMetadata(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"pr_url":          "https://example.com/pr/1",
				"pipeline_status": "waiting_review",
			},
		})
	})

	cmd := newTaskMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	out, runErr := captureStdout(t, func() error {
		return runTaskMetadataList(cmd, []string{testTaskUUID})
	})
	if runErr != nil {
		t.Fatalf("runTaskMetadataList: %v", runErr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON: %v\n%s", err, out)
	}
	if got["pr_url"] != "https://example.com/pr/1" || got["pipeline_status"] != "waiting_review" {
		t.Fatalf("stdout = %#v, missing expected keys", got)
	}
}

// 5xx and other non-404 errors must keep real error semantics — we only
// want to mask "this server has no metadata endpoint", not "the server
// is broken".
func TestRunTaskMetadataListPropagatesNon404Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	cmd := newTaskMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	// Drop stdout to keep test output clean even if the implementation
	// regresses and prints something.
	_, err := captureStdout(t, func() error {
		return runTaskMetadataList(cmd, []string{testTaskUUID})
	})
	if err == nil {
		t.Fatal("runTaskMetadataList returned nil on 500, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want it to mention status 500", err)
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error chain missing *cli.HTTPError: %v", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("HTTPError.StatusCode = %d, want 500", httpErr.StatusCode)
	}
}

func TestRunTaskMetadataListPropagates401Error(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})

	cmd := newTaskMetadataListTestCmd()
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runTaskMetadataList(cmd, []string{testTaskUUID})
	})
	if err == nil {
		t.Fatal("runTaskMetadataList returned nil on 401, want error")
	}
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 *cli.HTTPError, got %v", err)
	}
}

// get/set/delete must NOT degrade on 404 — those calls represent real
// caller intent and the user needs to see the failure.
func TestRunTaskMetadataGetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newTaskMetadataGetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runTaskMetadataGet(cmd, []string{testTaskUUID})
	})
	if err == nil {
		t.Fatal("runTaskMetadataGet returned nil on 404, want error")
	}
}

func TestRunTaskMetadataSetReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// PUT /metadata/<key> on an old server — must surface the failure.
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newTaskMetadataSetTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("value", "https://example.com/pr/1")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runTaskMetadataSet(cmd, []string{testTaskUUID})
	})
	if err == nil {
		t.Fatal("runTaskMetadataSet returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "set metadata") {
		t.Fatalf("error = %v, want it wrapped with 'set metadata' prefix", err)
	}
}

func TestRunTaskMetadataDeleteReturnsErrorOn404(t *testing.T) {
	_, _ = metadataTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	cmd := newTaskMetadataDeleteTestCmd()
	_ = cmd.Flags().Set("key", "pr_url")
	_ = cmd.Flags().Set("output", "json")

	_, err := captureStdout(t, func() error {
		return runTaskMetadataDelete(cmd, []string{testTaskUUID})
	})
	if err == nil {
		t.Fatal("runTaskMetadataDelete returned nil on 404, want error")
	}
	if !strings.Contains(err.Error(), "delete metadata") {
		t.Fatalf("error = %v, want it wrapped with 'delete metadata' prefix", err)
	}
}
