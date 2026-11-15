package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/silieco-ai/silieco/core/internal/analytics"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/realtime"
	"github.com/silieco-ai/silieco/core/internal/service"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

var testHandler *Handler
var testPool *pgxpool.Pool
var testUserID string
var testWorkspaceID string
var testRuntimeID string

const (
	handlerTestEmail         = "handler-test@silieco.ai"
	handlerTestName          = "Handler Test User"
	handlerTestWorkspaceSlug = "handler-tests"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://silieco:silieco@localhost:5432/silieco?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}

	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	testHandler = New(queries, pool, hub, bus, emailSvc, nil, nil, analytics.NoopClient{}, Config{AllowSignup: true})
	// httptest.NewRequest defaults RemoteAddr to 192.0.2.1, so every webhook
	// test in the suite shares one IP bucket. With the production default
	// (30/min) the budget runs out partway through the suite and unrelated
	// downstream tests see a 429 from the IP gate instead of the response
	// they're asserting. Tests that exercise rate limiting deliberately
	// swap in a tight limiter with t.Cleanup; this generous default keeps
	// the rest of the suite hermetic.
	testHandler.WebhookRateLimiter = NewMemoryWebhookRateLimiter(WebhookRateLimit{Limit: 1_000_000, Window: time.Minute})
	testHandler.WebhookIPRateLimiter = NewMemoryWebhookIPRateLimiter(WebhookRateLimit{Limit: 1_000_000, Window: time.Minute})
	testHandler.WebhookAbsoluteIPRateLimiter = NewMemoryWebhookAbsoluteIPRateLimiter(WebhookRateLimit{Limit: 1_000_000, Window: time.Minute})
	testPool = pool

	testUserID, testWorkspaceID, err = setupHandlerTestFixture(ctx, pool)
	if err != nil {
		fmt.Printf("Failed to set up handler test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()
	if err := cleanupHandlerTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean up handler test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupHandlerTestFixture(ctx context.Context, pool *pgxpool.Pool) (string, string, error) {
	if err := cleanupHandlerTestFixture(ctx, pool); err != nil {
		return "", "", err
	}

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, handlerTestName, handlerTestEmail).Scan(&userID); err != nil {
		return "", "", err
	}

	var workspaceID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Handler Tests", handlerTestWorkspaceSlug, "Temporary workspace for handler tests", "HAN").Scan(&workspaceID); err != nil {
		return "", "", err
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		return "", "", err
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, workspaceID, "Handler Test Runtime", "handler_test_runtime", "Handler test runtime", userID).Scan(&runtimeID); err != nil {
		return "", "", err
	}
	testRuntimeID = runtimeID

	var seededAgentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_runs, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4)
		RETURNING id
	`, workspaceID, "Handler Test Agent", runtimeID, userID).Scan(&seededAgentID); err != nil {
		return "", "", err
	}
	// SILI-3963: the seeded workspace-visible agent is invocable by workspace
	// members and A2A triggers, so seed its workspace invocation target.
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, seededAgentID, workspaceID); err != nil {
		return "", "", err
	}

	return userID, workspaceID, nil
}

func cleanupHandlerTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	var hasClientUsageTable bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('client_usage_daily') IS NOT NULL`).Scan(&hasClientUsageTable); err != nil {
		return err
	}
	if hasClientUsageTable {
		if _, err := pool.Exec(ctx, `DELETE FROM client_usage_daily WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, handlerTestEmail); err != nil {
			return err
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, handlerTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, handlerTestEmail); err != nil {
		return err
	}
	return nil
}

func newRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func withURLParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func setWorkspaceTaskPrefixForTest(t *testing.T, prefix string) {
	t.Helper()

	ctx := context.Background()
	var previous string
	if err := testPool.QueryRow(ctx, `SELECT task_prefix FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatalf("load workspace prefix: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET task_prefix = $1 WHERE id = $2`, prefix, testWorkspaceID); err != nil {
		t.Fatalf("set workspace prefix: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET task_prefix = $1 WHERE id = $2`, previous, testWorkspaceID)
	})
}

func handlerTestRuntimeID(t *testing.T) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent_runtime WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("failed to load handler test runtime: %v", err)
	}

	return runtimeID
}

func createHandlerTestAgent(t *testing.T, name string, mcpConfig []byte) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_runs, owner_id,
			instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4, '', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), testUserID, mcpConfig).Scan(&agentID); err != nil {
		t.Fatalf("failed to create handler test agent: %v", err)
	}
	// Generic test agents are workspace-invocable (SILI-3963): seed the
	// matching workspace invocation target so canInvokeAgent admits workspace
	// members and A2A triggers, mirroring the pre-permission-model behavior
	// where a workspace-visible agent could be triggered by anyone in the
	// workspace. Dedicated private-agent tests use privateAgentTestFixture.
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("failed to seed workspace invocation target: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	return agentID
}

// createHandlerTestTaskForAgent seeds a running agent_run row for the
// given agent (with no associated task) and returns the task UUID. Used by
// tests that need to set X-Run-ID alongside X-Agent-ID — resolveActor now
// requires the pair to be present and consistent before granting "agent"
// actor identity.
func createHandlerTestRunForAgent(t *testing.T, agentID string) string {
	return createHandlerTestRunForAgentOnTask(t, agentID, "")
}

// createHandlerTestRunForAgentOnTask seeds a running agent_run row
// for the given agent, optionally bound to an task (pass "" to leave
// task_id NULL). The bound-task form is needed by the self-loop guard
// test, which compares the calling task's task_id against the promoted
// task — only a same-task match counts as a true self-loop.
//
// Status is 'running' because X-Run-ID is something a currently-executing
// task sends. Using 'running' also keeps the seed outside the
// idx_one_pending_run_per_task_agent unique index (queued/dispatched only)
// and outside callers' `status='queued'` count assertions, so tests can
// assert that the handler did or did not enqueue a NEW task without
// double-counting the seed.
func createHandlerTestRunForAgentOnTask(t *testing.T, agentID, taskID string) string {
	t.Helper()

	var taskArg any
	if taskID == "" {
		taskArg = nil
	} else {
		taskArg = taskID
	}

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, started_at)
		VALUES ($1, $2, 'running', 0, $3, now())
		RETURNING id
	`, agentID, handlerTestRuntimeID(t), taskArg).Scan(&runID); err != nil {
		t.Fatalf("failed to create handler test run: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID)
	})
	return runID
}

func fetchAgentMcpConfig(t *testing.T, agentID string) []byte {
	t.Helper()

	var mcpConfig []byte
	if err := testPool.QueryRow(context.Background(), `SELECT mcp_config FROM agent WHERE id = $1`, agentID).Scan(&mcpConfig); err != nil {
		t.Fatalf("failed to load agent mcp_config: %v", err)
	}

	return mcpConfig
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("failed to unmarshal got JSON %q: %v", string(got), err)
	}

	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("failed to unmarshal want JSON %q: %v", want, err)
	}

	gotJSON, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("failed to marshal normalized got JSON: %v", err)
	}
	wantJSON, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("failed to marshal normalized want JSON: %v", err)
	}

	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("expected JSON %s, got %s", string(wantJSON), string(gotJSON))
	}
}

func TestTaskCRUD(t *testing.T) {
	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Test task from Go test",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Title != "Test task from Go test" {
		t.Fatalf("CreateTask: expected title 'Test task from Go test', got '%s'", created.Title)
	}
	if created.Status != "todo" {
		t.Fatalf("CreateTask: expected status 'todo', got '%s'", created.Status)
	}
	taskID := created.ID

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID, nil)
	req = withURLParam(req, "id", taskID)
	testHandler.GetTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var fetched TaskResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.ID != taskID {
		t.Fatalf("GetTask: expected id '%s', got '%s'", taskID, fetched.ID)
	}

	// Update - partial (only status)
	w = httptest.NewRecorder()
	status := "in_progress"
	req = newRequest("PUT", "/api/tasks/"+taskID, map[string]any{
		"status": status,
	})
	req = withURLParam(req, "id", taskID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated TaskResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != "in_progress" {
		t.Fatalf("UpdateTask: expected status 'in_progress', got '%s'", updated.Status)
	}
	if updated.Title != "Test task from Go test" {
		t.Fatalf("UpdateTask: title should be preserved, got '%s'", updated.Title)
	}
	if updated.Priority != "medium" {
		t.Fatalf("UpdateTask: priority should be preserved, got '%s'", updated.Priority)
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks?workspace_id="+testWorkspaceID, nil)
	testHandler.ListTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp map[string]any
	json.NewDecoder(w.Body).Decode(&listResp)
	tasks := listResp["tasks"].([]any)
	if len(tasks) == 0 {
		t.Fatal("ListTasks: expected at least 1 task")
	}

	// Delete
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+taskID, nil)
	req = withURLParam(req, "id", taskID)
	testHandler.DeleteTask(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteTask: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID, nil)
	req = withURLParam(req, "id", taskID)
	testHandler.GetTask(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetTask after delete: expected 404, got %d", w.Code)
	}
}

// TestDeleteTaskByIdentifier guards against #1661 — DELETE /api/tasks/{id}
// must actually delete the row when the path segment is a human-readable
// identifier ("HAN-42") rather than a UUID. Before the PR #1680 + SILI-1410
// refactor, parseUUID(rawString) silently produced a zero UUID, the SQL
// DELETE matched nothing, and the handler still returned 204.
//
// Also asserts the task:deleted WS event payload carries the resolved UUID,
// not the raw identifier — frontend caches key by UUID and would otherwise
// leave stale entries on other clients after an identifier-path delete.
func TestDeleteTaskByIdentifier(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Task to delete by identifier",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Identifier == "" {
		t.Fatalf("CreateTask: expected identifier to be populated, got empty")
	}

	// Capture the task:deleted event payload via the bus.
	gotPayload := make(chan map[string]any, 1)
	testHandler.Bus.Subscribe(protocol.EventTaskDeleted, func(e events.Event) {
		if payload, ok := e.Payload.(map[string]any); ok {
			select {
			case gotPayload <- payload:
			default:
			}
		}
	})

	// Delete using the human-readable identifier (e.g. "HAN-1") rather than the UUID.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+created.Identifier, nil)
	req = withURLParam(req, "id", created.Identifier)
	testHandler.DeleteTask(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteTask by identifier: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the row is actually gone — the silent-data-loss bug would have
	// returned 204 here too, but the row would still exist.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM task WHERE id = $1`, created.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("DeleteTask by identifier returned 204 but row still exists (count=%d) — silent-data-loss regression", count)
	}

	// Event payload must carry the resolved UUID, not the identifier string.
	select {
	case payload := <-gotPayload:
		taskID, _ := payload["task_id"].(string)
		if taskID != created.ID {
			t.Fatalf("task:deleted event payload task_id = %q; want resolved UUID %q (must not leak identifier %q)", taskID, created.ID, created.Identifier)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive task:deleted event within timeout")
	}
}

// TestDeleteTaskRejectsInvalidUUID verifies that a path segment that is
// neither a valid UUID nor a valid identifier returns 404 (not 204) — the
// handler must never silently succeed on malformed input.
func TestDeleteTaskRejectsInvalidUUID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/tasks/not-a-uuid-or-identifier", nil)
	req = withURLParam(req, "id", "not-a-uuid-or-identifier")
	testHandler.DeleteTask(w, req)
	if w.Code == http.StatusNoContent {
		t.Fatalf("DeleteTask with invalid id: must not return 204; got %d", w.Code)
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeleteTask with invalid id: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskDefaultStatusIsTodo verifies that tasks created without an
// explicit status default to "todo" so the daemon picks them up immediately.
// Before this fix the default was "backlog", which daemons ignore.
func TestCreateTaskDefaultStatusIsTodo(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Task with no explicit status",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Status != "todo" {
		t.Fatalf("CreateTask: expected default status 'todo', got '%s'", created.Status)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
}

// TestCreateTaskExplicitBacklogPreserved verifies that explicitly requesting
// "backlog" status is still respected — only the implicit default changed.
func TestCreateTaskExplicitBacklogPreserved(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Explicit backlog task",
		"status": "backlog",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Status != "backlog" {
		t.Fatalf("CreateTask: expected explicit 'backlog' to be preserved, got '%s'", created.Status)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
}

// TestCreateTaskRejectsCrossWorkspaceParent guards the workspace
// boundary check that lives in service.TaskService.Create. A request
// that pins parent_task_id to an task in a foreign workspace must be
// rejected before the row is created — this is the structural reason
// TaskService owns the parent lookup (not the HTTP handler). The test
// inserts a foreign workspace + task directly via SQL, then drives the
// request through the regular handler entry point.
func TestCreateTaskRejectsCrossWorkspaceParent(t *testing.T) {
	ctx := context.Background()

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Cross-workspace parent test", "xwp-parent-test", "Foreign workspace", "XWP").Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	var foreignParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 1)
		RETURNING id
	`, otherWorkspaceID, "Foreign parent", testUserID).Scan(&foreignParentID); err != nil {
		t.Fatalf("insert foreign parent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Should be rejected",
		"parent_task_id": foreignParentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask with foreign parent: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "parent task not found in this workspace") {
		t.Fatalf("CreateTask with foreign parent: expected boundary error message, got %s", w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM task WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, "Should be rejected",
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected create still wrote a row (count=%d) — service-layer boundary check failed", count)
	}
}

// TestCreateTaskRejectsCrossWorkspaceProject mirrors the parent test for
// the project workspace boundary. Same reasoning: future create entries
// (Lark /task, MCP, API keys) must inherit this guard from the service
// without re-implementing it.
func TestCreateTaskRejectsCrossWorkspaceProject(t *testing.T) {
	ctx := context.Background()

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Cross-workspace project test", "xwp-project-test", "Foreign workspace", "XWP").Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	var foreignProjectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status, priority)
		VALUES ($1, $2, 'planned', 'none')
		RETURNING id
	`, otherWorkspaceID, "Foreign project").Scan(&foreignProjectID); err != nil {
		t.Fatalf("insert foreign project: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Should be rejected",
		"project_id": foreignProjectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask with foreign project: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "project not found in this workspace") {
		t.Fatalf("CreateTask with foreign project: expected boundary error message, got %s", w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM task WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, "Should be rejected",
	).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejected create still wrote a row (count=%d) — service-layer boundary check failed", count)
	}
}

func TestCreateSubTaskInheritsParentProject(t *testing.T) {
	var projectID, parentID, childID string
	defer func() {
		for _, taskID := range []string{childID, parentID} {
			if taskID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/tasks/"+taskID, nil)
			req = withURLParam(req, "id", taskID)
			testHandler.DeleteTask(httptest.NewRecorder(), req)
		}
		if projectID != "" {
			req := newRequest("DELETE", "/api/projects/"+projectID, nil)
			req = withURLParam(req, "id", projectID)
			testHandler.DeleteProject(httptest.NewRecorder(), req)
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Sub-task inheritance project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	projectID = project.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Parent with project",
		"project_id": projectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent TaskResponse
	json.NewDecoder(w.Body).Decode(&parent)
	parentID = parent.ID
	if parent.ProjectID == nil || *parent.ProjectID != projectID {
		t.Fatalf("CreateTask parent: expected project_id %q, got %v", projectID, parent.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Child without explicit project",
		"parent_task_id": parentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child TaskResponse
	json.NewDecoder(w.Body).Decode(&child)
	childID = child.ID

	if child.ParentTaskID == nil || *child.ParentTaskID != parentID {
		t.Fatalf("CreateTask child: expected parent_task_id %q, got %v", parentID, child.ParentTaskID)
	}
	if child.ProjectID == nil || *child.ProjectID != projectID {
		t.Fatalf("CreateTask child: expected inherited project_id %q, got %v", projectID, child.ProjectID)
	}
}

func TestCreateSubTaskUsesExplicitProjectOverParentProject(t *testing.T) {
	var parentProjectID, childProjectID, parentID, childID string
	defer func() {
		for _, taskID := range []string{childID, parentID} {
			if taskID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/tasks/"+taskID, nil)
			req = withURLParam(req, "id", taskID)
			testHandler.DeleteTask(httptest.NewRecorder(), req)
		}
		for _, projectID := range []string{childProjectID, parentProjectID} {
			if projectID == "" {
				continue
			}
			req := newRequest("DELETE", "/api/projects/"+projectID, nil)
			req = withURLParam(req, "id", projectID)
			testHandler.DeleteProject(httptest.NewRecorder(), req)
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Parent project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parentProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&parentProject)
	parentProjectID = parentProject.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Child explicit project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var childProject ProjectResponse
	json.NewDecoder(w.Body).Decode(&childProject)
	childProjectID = childProject.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Parent with project",
		"project_id": parentProjectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent TaskResponse
	json.NewDecoder(w.Body).Decode(&parent)
	parentID = parent.ID
	if parent.ProjectID == nil || *parent.ProjectID != parentProjectID {
		t.Fatalf("CreateTask parent: expected project_id %q, got %v", parentProjectID, parent.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Child with explicit project",
		"parent_task_id": parentID,
		"project_id":     childProjectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child TaskResponse
	json.NewDecoder(w.Body).Decode(&child)
	childID = child.ID

	if child.ParentTaskID == nil || *child.ParentTaskID != parentID {
		t.Fatalf("CreateTask child: expected parent_task_id %q, got %v", parentID, child.ParentTaskID)
	}
	if child.ProjectID == nil || *child.ProjectID != childProjectID {
		t.Fatalf("CreateTask child: expected explicit project_id %q, got %v", childProjectID, child.ProjectID)
	}
}

func TestCreateTaskRejectsActiveDuplicate(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var projectID, parentID, taskID, duplicateID string
	defer func() {
		for _, id := range []string{duplicateID, taskID, parentID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, id)
			}
		}
		if projectID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		}
	}()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, "Duplicate guard project "+suffix).Scan(&projectID); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Duplicate guard parent " + suffix,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	parentID = parent.ID

	title := "SH-PM-SYNTH-01 Synthesize recommendation-to-shortlist planning outputs " + suffix
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          title,
		"status":         "in_progress",
		"parent_task_id": parentID,
		"project_id":     projectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask original: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var original TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&original); err != nil {
		t.Fatalf("decode original: %v", err)
	}
	taskID = original.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "  sh-pm-synth-01   synthesize recommendation-to-shortlist planning outputs " + suffix + "  ",
		"parent_task_id": parentID,
		"project_id":     projectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateTask duplicate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var conflict struct {
		Code  string       `json:"code"`
		Error string       `json:"error"`
		Task  TaskResponse `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode conflict: %v", err)
	}
	if conflict.Code != "active_duplicate_task" {
		t.Fatalf("code = %q, want active_duplicate_task", conflict.Code)
	}
	if conflict.Task.ID != taskID || conflict.Task.Status != "in_progress" {
		t.Fatalf("conflict task = %#v, want original %s in_progress", conflict.Task, taskID)
	}
	if !strings.Contains(conflict.Error, original.Identifier+" "+title) || !strings.Contains(conflict.Error, "allow_duplicate=true") || !strings.Contains(conflict.Error, "--allow-duplicate") {
		t.Fatalf("unexpected duplicate message: %q", conflict.Error)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":           title,
		"parent_task_id":  parentID,
		"project_id":      projectID,
		"allow_duplicate": true,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask allow duplicate: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var duplicate TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode duplicate: %v", err)
	}
	duplicateID = duplicate.ID
	if duplicateID == taskID {
		t.Fatalf("allow duplicate returned original task id %s", duplicateID)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          title,
		"parent_task_id": parentID,
		"project_id":     projectID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateTask duplicate after allow-duplicate: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&conflict); err != nil {
		t.Fatalf("decode second conflict: %v", err)
	}
	if conflict.Task.ID != taskID {
		t.Fatalf("conflict task = %s, want oldest active task %s", conflict.Task.ID, taskID)
	}
}

func TestCreateTaskAllowsDuplicateAfterCancelled(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Cancelled duplicate guard %d", time.Now().UnixNano())
	var firstID, secondID string
	defer func() {
		for _, id := range []string{secondID, firstID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, id)
			}
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "cancelled",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask cancelled: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode cancelled: %v", err)
	}
	firstID = first.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask after cancelled: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var second TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	secondID = second.ID
	if secondID == firstID {
		t.Fatalf("new task reused cancelled task id %s", secondID)
	}
}

func TestCreateTaskAllowsDuplicateAfterDone(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Done duplicate guard %d", time.Now().UnixNano())
	var firstID, secondID string
	defer func() {
		for _, id := range []string{secondID, firstID} {
			if id != "" {
				testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, id)
			}
		}
	}()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "done",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask done: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode done: %v", err)
	}
	firstID = first.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask after done: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var second TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	secondID = second.ID
	if secondID == firstID {
		t.Fatalf("new task reused done task id %s", secondID)
	}
}

func TestTriggerAutopilotAllowsActiveDuplicateTask(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot duplicate task %d", time.Now().UnixNano())
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		testPool.Exec(ctx, `DELETE FROM task WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask existing: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var existing TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&existing); err != nil {
		t.Fatalf("decode existing task: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Duplicate title autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", autopilotID)
	testHandler.TriggerAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("TriggerAutopilot duplicate title: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var run AutopilotRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode autopilot run: %v", err)
	}
	if run.Status != "task_created" {
		t.Fatalf("run status = %q, want task_created", run.Status)
	}
	if run.TaskID == nil {
		t.Fatal("run task_id is nil, want newly created task")
	}
	if *run.TaskID == existing.ID {
		t.Fatalf("run reused existing task %s, want a new task", existing.ID)
	}
	if run.FailureReason != nil {
		t.Fatalf("run failure_reason = %q, want nil", *run.FailureReason)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 2 {
		t.Fatalf("autopilot should create a new same-title task, got %d matching tasks", count)
	}
}

func TestScheduledAutopilotAllowsActiveDuplicateTask(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Scheduled autopilot duplicate task %d", time.Now().UnixNano())
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		testPool.Exec(ctx, `DELETE FROM task WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask existing: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var existing TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&existing); err != nil {
		t.Fatalf("decode existing task: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Scheduled duplicate title autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "schedule", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot schedule duplicate: %v", err)
	}
	if run == nil || run.Status != "task_created" {
		t.Fatalf("dispatch result = %+v, want status task_created", run)
	}
	newTaskID := uuidToString(run.TaskID)
	if newTaskID == "" {
		t.Fatal("run task_id is empty, want newly created task")
	}
	if newTaskID == existing.ID {
		t.Fatalf("run reused existing task %s, want a new task", existing.ID)
	}
	if run.FailureReason.Valid {
		t.Fatalf("run failure_reason = %q, want empty", run.FailureReason.String)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 2 {
		t.Fatalf("autopilot should create a new same-title task, got %d matching tasks", count)
	}
}

// TestAutopilotCreatedTaskCreatorIsAssigneeAgent locks in that an task spawned
// by an autopilot reports the assignee agent — not the human who configured the
// autopilot — as its creator. The matching task:created event must carry the
// same actor identity so downstream activity / notification listeners stay in
// sync with the task row.
func TestAutopilotCreatedTaskCreatorIsAssigneeAgent(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot creator attribution %d", time.Now().UnixNano())
	var autopilotID, taskID string
	defer func() {
		if taskID != "" {
			testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Creator attribution autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID
	if autopilot.CreatedByType != "member" || autopilot.CreatedByID != testUserID {
		t.Fatalf("autopilot created_by = %s/%s, want member/%s", autopilot.CreatedByType, autopilot.CreatedByID, testUserID)
	}

	gotEvent := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventTaskCreated, func(e events.Event) {
		select {
		case gotEvent <- e:
		default:
		}
	})

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || run.Status != "task_created" {
		t.Fatalf("dispatch result = %+v, want status task_created", run)
	}

	var creatorType, creatorID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, creator_type, creator_id
		FROM task
		WHERE workspace_id = $1 AND title = $2
		ORDER BY created_at DESC
		LIMIT 1
	`, testWorkspaceID, title).Scan(&taskID, &creatorType, &creatorID); err != nil {
		t.Fatalf("load autopilot-created task: %v", err)
	}
	if creatorType != "agent" {
		t.Fatalf("task creator_type = %q, want agent", creatorType)
	}
	if creatorID != agentID {
		t.Fatalf("task creator_id = %q, want assignee agent %q", creatorID, agentID)
	}

	select {
	case ev := <-gotEvent:
		if ev.ActorType != "agent" {
			t.Fatalf("task:created ActorType = %q, want agent", ev.ActorType)
		}
		if ev.ActorID != agentID {
			t.Fatalf("task:created ActorID = %q, want %q", ev.ActorID, agentID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive task:created event")
	}
}

func TestAutopilotCreateTaskAssociatesConfiguredProject(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot project task %d", time.Now().UnixNano())
	var autopilotID, taskID, projectID string
	defer func() {
		if taskID != "" {
			testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if projectID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		}
	}()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id::text
	`, testWorkspaceID, "Autopilot project target").Scan(&projectID); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Project-linked autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": title,
		"project_id":          projectID,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID
	if autopilot.ProjectID == nil || *autopilot.ProjectID != projectID {
		t.Fatalf("autopilot project_id = %v, want %q", autopilot.ProjectID, projectID)
	}

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
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
	taskID = uuidToString(run.TaskID)

	var taskProjectID *string
	if err := testPool.QueryRow(ctx, `
		SELECT project_id::text
		FROM task
		WHERE id = $1
	`, taskID).Scan(&taskProjectID); err != nil {
		t.Fatalf("load created task project: %v", err)
	}
	if taskProjectID == nil || *taskProjectID != projectID {
		t.Fatalf("created task project_id = %v, want %q", taskProjectID, projectID)
	}
}

func TestAutopilotDispatchUsesCurrentProjectBinding(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot stale project task %d", time.Now().UnixNano())
	var autopilotID, taskID, projectAID, projectBID string
	defer func() {
		if taskID != "" {
			testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if projectAID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectAID)
		}
		if projectBID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectBID)
		}
	}()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id::text
	`, testWorkspaceID, "Autopilot stale project A").Scan(&projectAID); err != nil {
		t.Fatalf("create project A fixture: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id::text
	`, testWorkspaceID, "Autopilot stale project B").Scan(&projectBID); err != nil {
		t.Fatalf("create project B fixture: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "Stale-project autopilot",
		"assignee_id":         agentID,
		"execution_mode":      "create_task",
		"task_title_template": title,
		"project_id":          projectAID,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = created.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectBID,
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.TaskID.Valid {
		t.Fatalf("dispatch run = %+v, want linked task", run)
	}
	taskID = uuidToString(run.TaskID)

	var taskProjectID *string
	if err := testPool.QueryRow(ctx, `
		SELECT project_id::text
		FROM task
		WHERE id = $1
	`, taskID).Scan(&taskProjectID); err != nil {
		t.Fatalf("load created task project: %v", err)
	}
	if taskProjectID == nil || *taskProjectID != projectBID {
		t.Fatalf("created task project_id = %v, want refreshed %q", taskProjectID, projectBID)
	}
}

func TestUpdateAutopilotCanSetAndClearProject(t *testing.T) {
	ctx := context.Background()
	var autopilotID, projectID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
		if projectID != "" {
			testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
		}
	}()

	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id::text
	`, testWorkspaceID, "Autopilot update project target").Scan(&projectID); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Project update autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_task",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created autopilot: %v", err)
	}
	autopilotID = created.ID
	if created.ProjectID != nil {
		t.Fatalf("new autopilot project_id = %v, want nil", created.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot set project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated autopilot: %v", err)
	}
	if updated.ProjectID == nil || *updated.ProjectID != projectID {
		t.Fatalf("updated project_id = %v, want %q", updated.ProjectID, projectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": nil,
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot clear project: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cleared AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode cleared autopilot: %v", err)
	}
	if cleared.ProjectID != nil {
		t.Fatalf("cleared project_id = %v, want nil", cleared.ProjectID)
	}
}

// TestCreateTaskRejectsNonexistentMemberAssignee covers the bug where any
// well-formed UUID was accepted as assignee_id without checking workspace
// membership.
func TestCreateTaskRejectsNonexistentMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Ghost member assignee",
		"assignee_type": "member",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 for nonexistent member, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskRejectsNonexistentAgentAssignee verifies the same check on
// the agent branch — previously rejected with 403 "agent not found"; we want a
// consistent 400 from the new validator.
func TestCreateTaskRejectsNonexistentAgentAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Ghost agent assignee",
		"assignee_type": "agent",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 for nonexistent agent, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskRejectsAssigneeTypeWithoutID rejects requests where only one
// of the two fields was supplied — historically this would create an task
// with an inconsistent state.
func TestCreateTaskRejectsAssigneeTypeWithoutID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Lone assignee_type",
		"assignee_type": "member",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 when only assignee_type is set, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskRejectsAssigneeIDWithoutType is the symmetric case.
func TestCreateTaskRejectsAssigneeIDWithoutType(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Lone assignee_id",
		"assignee_id": testUserID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 when only assignee_id is set, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskRejectsUnknownAssigneeType guards against typos like
// "members" or "user" that previously sneaked through.
func TestCreateTaskRejectsUnknownAssigneeType(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Bogus assignee_type",
		"assignee_type": "user",
		"assignee_id":   testUserID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 for unknown assignee_type, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTaskAcceptsValidMemberAssignee is the positive control — the
// validator must not block legitimate workspace members.
func TestCreateTaskAcceptsValidMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Valid member assignee",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201 for valid member assignee, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
}

// TestCreateTaskRejectsMalformedAssigneeID covers the case where parseUUID
// silently produces an invalid pgtype.UUID and the validator would otherwise
// treat (no type + unparseable id) as "no assignee" and accept the request.
func TestCreateTaskRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":       "Malformed assignee_id only",
		"assignee_id": "not-a-uuid",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateTaskRejectsMalformedAttachmentIDBeforeWrite(t *testing.T) {
	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM task WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count tasks before: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Malformed attachment task",
		"attachment_ids": []string{"not-a-uuid"},
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateTask: expected 400 for malformed attachment_ids, got %d: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM task WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count tasks after: %v", err)
	}
	if after != before {
		t.Fatalf("CreateTask: malformed attachment_ids should not create task, count before=%d after=%d", before, after)
	}
}

// TestUpdateTaskRejectsMalformedAssigneeID is the equivalent for the update
// path, where the same parseUUID-shaped gap existed on a previously-unassigned
// task.
func TestUpdateTaskRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Update malformed assignee target",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{
		"assignee_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateTask: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTaskRejectsNonexistentMemberAssignee verifies the same gap is
// closed on the update path — UpdateTask previously only validated agents.
func TestUpdateTaskRejectsNonexistentMemberAssignee(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Update assignee target",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{
		"assignee_type": "member",
		"assignee_id":   "00000000-0000-0000-0000-000000000000",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateTask: expected 400 for nonexistent member, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTaskAllowsExplicitUnassign verifies that sending null for both
// fields still works after the new validator landed — clearing the assignee
// must not be misclassified as a mismatched pair.
func TestUpdateTaskAllowsExplicitUnassign(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Task to unassign",
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer func() {
		cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
	}()

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{
		"assignee_type": nil,
		"assignee_id":   nil,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200 for unassign, got %d: %s", w.Code, w.Body.String())
	}
	var updated TaskResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.AssigneeType != nil || updated.AssigneeID != nil {
		t.Fatalf("UpdateTask: expected assignee cleared, got type=%v id=%v", updated.AssigneeType, updated.AssigneeID)
	}
}

func TestCommentCRUD(t *testing.T) {
	// Create an task first
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Comment test task",
	})
	testHandler.CreateTask(w, req)
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	taskID := task.ID

	// Create comment
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "Test comment from Go test",
	})
	req = withURLParam(req, "id", taskID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List comments
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/tasks/"+taskID+"/comments", nil)
	req = withURLParam(req, "id", taskID)
	testHandler.ListComments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListComments: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var comments []CommentResponse
	json.NewDecoder(w.Body).Decode(&comments)
	if len(comments) != 1 {
		t.Fatalf("ListComments: expected 1 comment, got %d", len(comments))
	}
	if comments[0].Content != "Test comment from Go test" {
		t.Fatalf("ListComments: expected content 'Test comment from Go test', got '%s'", comments[0].Content)
	}

	// Cleanup
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+taskID, nil)
	req = withURLParam(req, "id", taskID)
	testHandler.DeleteTask(w, req)
}

func TestCommentWritePathsPreserveTaskIdentifiers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("requires DB")
	}

	ctx := context.Background()
	setWorkspaceTaskPrefixForTest(t, "MUL")

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, number)
		VALUES ($1, 'member', $2, $3, 3310)
		RETURNING id
	`, testWorkspaceID, testUserID, "preserve bare task identifiers").Scan(&taskID); err != nil {
		t.Fatalf("create task fixture: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	explicitMention := fmt.Sprintf("[SILI-3310](mention://task/%s)", taskID)
	createCases := []string{
		"SILI-3310",
		"task/SILI-3310",
		"feature/SILI-3310",
		explicitMention,
	}

	var firstCommentID string
	for _, content := range createCases {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
			"content": content,
		})
		req = withURLParam(req, "id", taskID)
		testHandler.CreateComment(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment(%q): expected 201, got %d: %s", content, w.Code, w.Body.String())
		}

		var created CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatalf("decode created comment: %v", err)
		}
		if created.Content != content {
			t.Fatalf("CreateComment(%q) stored %q", content, created.Content)
		}
		if firstCommentID == "" {
			firstCommentID = created.ID
		}
	}

	updatedContent := "updated SILI-3310 task/SILI-3310 feature/SILI-3310 " + explicitMention
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/comments/"+firstCommentID, map[string]any{
		"content": updatedContent,
	})
	req = withURLParam(req, "commentId", firstCommentID)
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated comment: %v", err)
	}
	if updated.Content != updatedContent {
		t.Fatalf("UpdateComment stored %q, want %q", updated.Content, updatedContent)
	}
}

func TestCreateCommentRejectsMalformedParentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Comment malformed parent task",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks/"+task.ID+"/comments", map[string]any{
		"content":   "bad parent",
		"parent_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", task.ID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateComment: expected 400 for malformed parent_id, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/tasks/"+task.ID, nil)
	req = withURLParam(req, "id", task.ID)
	testHandler.DeleteTask(w, req)
}

func TestGetChatSessionRejectsMalformedSessionID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/chat/sessions/not-a-uuid", nil)
	req = withURLParam(req, "sessionId", "not-a-uuid")
	testHandler.GetChatSession(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetChatSession: expected 400 for malformed sessionId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotRejectsMalformedAssigneeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots", map[string]any{
		"title":          "Malformed assignee autopilot",
		"assignee_id":    "not-a-uuid",
		"execution_mode": "run_only",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for malformed assignee_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilotRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/autopilots/not-a-uuid", map[string]any{
		"title": "Malformed autopilot id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAutopilot: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRejectsMalformedAgentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/not-a-uuid", map[string]any{
		"name": "Malformed agent id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgentRejectsMalformedRuntimeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents", map[string]any{
		"name":       "Malformed runtime agent",
		"runtime_id": "not-a-uuid",
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAgent: expected 400 for malformed runtime_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRejectsMalformedRuntimeID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Malformed Runtime Update", nil)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"runtime_id": "not-a-uuid",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400 for malformed runtime_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreatePinRejectsMalformedItemID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/pins", map[string]any{
		"item_type": "task",
		"item_id":   "not-a-uuid",
	})
	testHandler.CreatePin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreatePin: expected 400 for malformed item_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateWorkspaceRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/workspaces/not-a-uuid", map[string]any{
		"name": "Malformed workspace id",
	})
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.UpdateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateWorkspace: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberRejectsMalformedMemberID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+testWorkspaceID+"/members/not-a-uuid", map[string]any{
		"role": "member",
	})
	req = withURLParams(req, "id", testWorkspaceID, "memberId", "not-a-uuid")
	testHandler.UpdateMember(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateMember: expected 400 for malformed memberId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeInvitationRejectsMalformedInvitationID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/invitations/not-a-uuid", nil)
	req = withURLParams(req, "id", testWorkspaceID, "invitationId", "not-a-uuid")
	testHandler.RevokeInvitation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("RevokeInvitation: expected 400 for malformed invitationId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetMyInvitationRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/invitations/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.GetMyInvitation(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetMyInvitation: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddReactionRejectsMalformedCommentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/comments/not-a-uuid/reactions", map[string]any{
		"emoji": "thumbs_up",
	})
	req = withURLParam(req, "commentId", "not-a-uuid")
	testHandler.AddReaction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddReaction: expected 400 for malformed commentId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCommentRejectsMalformedCommentID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/comments/not-a-uuid", map[string]any{
		"content": "updated",
	})
	req = withURLParam(req, "commentId", "not-a-uuid")
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateComment: expected 400 for malformed commentId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMarkInboxReadRejectsMalformedItemID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/inbox/not-a-uuid/read", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.MarkInboxRead(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("MarkInboxRead: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokePersonalAccessTokenRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/tokens/not-a-uuid", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	testHandler.RevokePersonalAccessToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("RevokePersonalAccessToken: expected 400 for malformed id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequestBodyUUIDFieldsRejectMalformed(t *testing.T) {
	tests := []struct {
		name   string
		req    *http.Request
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "daemon register workspace_id",
			req: newRequest("POST", "/api/daemon/register", map[string]any{
				"workspace_id": "not-a-uuid",
				"daemon_id":    "daemon-malformed-workspace",
				"runtimes": []map[string]any{
					{"name": "codex", "type": "codex", "status": "online"},
				},
			}),
			handle: testHandler.DaemonRegister,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handle(w, tt.req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400 for malformed body UUID, got %d: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestDaemonDeregisterRejectsMalformedRuntimeID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/daemon/deregister", map[string]any{
		"runtime_ids": []string{"not-a-uuid"},
	})
	testHandler.DaemonDeregister(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DaemonDeregister: expected 400 for malformed runtime_ids, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetTaskGCCheckRejectsMalformedTaskID(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/daemon/runs/not-a-uuid/gc-check", nil)
	req = withURLParam(req, "taskId", "not-a-uuid")
	testHandler.GetTaskGCCheck(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("GetTaskGCCheck: expected 400 for malformed taskId, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBatchTaskGCCheckRejectsInvalidRequests(t *testing.T) {
	h := &Handler{}
	workspaceID := "00000000-0000-0000-0000-000000000001"
	tooMany := make([]string, maxTaskGCBatchSize+1)
	for i := range tooMany {
		tooMany[i] = "00000000-0000-0000-0000-000000000002"
	}

	tests := []struct {
		name        string
		workspaceID string
		body        any
	}{
		{name: "malformed workspace", workspaceID: "not-a-uuid", body: map[string]any{"task_ids": []string{}}},
		{name: "malformed task", workspaceID: workspaceID, body: map[string]any{"task_ids": []string{"not-a-uuid"}}},
		{name: "too many tasks", workspaceID: workspaceID, body: map[string]any{"task_ids": tooMany}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newDaemonTokenRequest("POST", "/api/daemon/workspaces/"+tt.workspaceID+"/tasks/gc-check", tt.body,
				tt.workspaceID, "test-daemon")
			req = withURLParam(req, "workspaceId", tt.workspaceID)
			h.BatchTaskGCCheck(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSetAgentSkillsRejectsMalformedSkillID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Malformed Skill Assignment", nil)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID+"/skills", map[string]any{
		"skill_ids": []string{"not-a-uuid"},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.SetAgentSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("SetAgentSkills: expected 400 for malformed skill_ids, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddAgentSkillsPreservesExistingAssignments(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Add Skill Preserves Existing", nil)
	existingSkillID := insertHandlerTestSkill(t, "add-preserve-existing", "existing body")
	newSkillID := insertHandlerTestSkill(t, "add-preserve-new", "new body")

	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, existingSkillID,
	); err != nil {
		t.Fatalf("seed existing skill assignment: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents/"+agentID+"/skills/add", map[string]any{
		"skill_ids": []string{newSkillID},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentSkills(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AddAgentSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp []SkillSummaryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	assertSkillIDsPresent(t, resp, existingSkillID, newSkillID)
	assertAgentSkillRowCount(t, agentID, 2)
}

func TestAddAgentSkillsAddsMultipleAndIsIdempotent(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Add Multiple Skills", nil)
	skillA := insertHandlerTestSkill(t, "add-multiple-a", "a body")
	skillB := insertHandlerTestSkill(t, "add-multiple-b", "b body")

	for attempt := 0; attempt < 2; attempt++ {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/agents/"+agentID+"/skills/add", map[string]any{
			"skill_ids": []string{skillA, skillB},
		})
		req = withURLParam(req, "id", agentID)
		testHandler.AddAgentSkills(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("AddAgentSkills attempt %d: expected 200, got %d: %s", attempt+1, w.Code, w.Body.String())
		}
	}

	assertAgentSkillRowCount(t, agentID, 2)
}

func TestAddAgentSkillsRejectsMalformedSkillID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Add Malformed Skill Assignment", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents/"+agentID+"/skills/add", map[string]any{
		"skill_ids": []string{"not-a-uuid"},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AddAgentSkills: expected 400 for malformed skill_ids, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddAgentSkillsRejectsCrossWorkspaceSkillID(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Add Cross Workspace Skill", nil)
	foreignSkillID := insertHandlerTestSkillInForeignWorkspace(t, "add-cross-workspace", "foreign body")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents/"+agentID+"/skills/add", map[string]any{
		"skill_ids": []string{foreignSkillID},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.AddAgentSkills(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("AddAgentSkills: expected 404 for cross-workspace skill_id, got %d: %s", w.Code, w.Body.String())
	}
	assertAgentSkillRowCount(t, agentID, 0)
}

func insertHandlerTestSkillInForeignWorkspace(t *testing.T, namePrefix, content string) string {
	t.Helper()
	ctx := context.Background()
	slug := "foreign-skill-" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Foreign Skill Workspace "+t.Name(), slug, "", "FSW").Scan(&workspaceID); err != nil {
		t.Fatalf("insert foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	name := namePrefix + "-" + t.Name()
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
		RETURNING id
	`, workspaceID, name, "fixture", content, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert foreign skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

func assertSkillIDsPresent(t *testing.T, skills []SkillSummaryResponse, wantIDs ...string) {
	t.Helper()
	got := make(map[string]bool, len(skills))
	for _, s := range skills {
		got[s.ID] = true
	}
	for _, want := range wantIDs {
		if !got[want] {
			t.Fatalf("response missing skill %s; got %+v", want, skills)
		}
	}
}

func assertAgentSkillRowCount(t *testing.T, agentID string, want int) {
	t.Helper()
	var got int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1`,
		agentID,
	).Scan(&got); err != nil {
		t.Fatalf("count agent_skill: %v", err)
	}
	if got != want {
		t.Fatalf("agent_skill row count: got %d, want %d", got, want)
	}
}

func TestAgentCRUD(t *testing.T) {
	// List agents
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	testHandler.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agents []AgentResponse
	json.NewDecoder(w.Body).Decode(&agents)
	if len(agents) == 0 {
		t.Fatal("ListAgents: expected at least 1 agent")
	}

	// Update agent status
	agentID := agents[0].ID
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"status": "idle",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Status != "idle" {
		t.Fatalf("UpdateAgent: expected status 'idle', got '%s'", updated.Status)
	}
	if updated.Name != agents[0].Name {
		t.Fatalf("UpdateAgent: name should be preserved, got '%s'", updated.Name)
	}
}

func TestUpdateAgentMcpConfigAbsentPreservesValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Preserve", []byte(`{"preset":"keep"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"name": "Handler Mcp Preserve Updated",
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `{"preset":"keep"}`)
	assertJSONEqual(t, fetchAgentMcpConfig(t, agentID), `{"preset":"keep"}`)
}

func TestUpdateAgentMcpConfigNullClearsValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Clear", []byte(`{"preset":"clear"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"mcp_config": nil,
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `null`)
	if fetchAgentMcpConfig(t, agentID) != nil {
		t.Fatalf("UpdateAgent: expected DB mcp_config to be SQL NULL")
	}
}

func TestUpdateAgentMcpConfigObjectUpdatesValue(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Mcp Update", []byte(`{"preset":"old"}`))

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/agents/"+agentID, map[string]any{
		"mcp_config": map[string]any{"preset": "new"},
	})
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("UpdateAgent: decode response: %v", err)
	}
	assertJSONEqual(t, updated.McpConfig, `{"preset":"new"}`)
	assertJSONEqual(t, fetchAgentMcpConfig(t, agentID), `{"preset":"new"}`)
}

func TestCreateAgentMcpConfigNullStoresSQLNull(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents", map[string]any{
		"name":        "Handler Mcp Create Null",
		"runtime_id":  handlerTestRuntimeID(t),
		"mcp_config":  nil,
		"custom_env":  map[string]string{},
		"custom_args": []string{},
	})
	testHandler.CreateAgent(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	assertJSONEqual(t, created.McpConfig, `null`)
	if fetchAgentMcpConfig(t, created.ID) != nil {
		t.Fatalf("CreateAgent: expected DB mcp_config to be SQL NULL")
	}
}

func TestWorkspaceCRUD(t *testing.T) {
	// List workspaces
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/workspaces", nil)
	testHandler.ListWorkspaces(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaces: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var workspaces []WorkspaceResponse
	json.NewDecoder(w.Body).Decode(&workspaces)
	if len(workspaces) == 0 {
		t.Fatal("ListWorkspaces: expected at least 1 workspace")
	}

	// Get workspace
	wsID := workspaces[0].ID
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.GetWorkspace(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateWorkspaceUsesRequestedSlug(t *testing.T) {
	const slug = "handler-create-workspace-requested"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Handler Create Workspace Requested",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created WorkspaceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateWorkspace: decode response: %v", err)
	}
	if created.Slug != slug {
		t.Fatalf("CreateWorkspace: expected slug %q, got %q", slug, created.Slug)
	}
}

func TestCreateWorkspaceSlugConflictReturnsConflict(t *testing.T) {
	ctx := context.Background()
	retriedSlug := handlerTestWorkspaceSlug + "-2"

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, retriedSlug)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Duplicate Handler Workspace",
		"slug": handlerTestWorkspaceSlug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("CreateWorkspace: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, retriedSlug).Scan(&count); err != nil {
		t.Fatalf("CreateWorkspace: check retried slug: %v", err)
	}
	if count != 0 {
		t.Fatalf("CreateWorkspace: expected no fallback slug %q, got %d rows", retriedSlug, count)
	}
}

func TestCreateWorkspaceInvalidSlugReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]string{
		"name": "Invalid Slug Workspace",
		"slug": "invalid slug",
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateWorkspace: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendCode(t *testing.T) {
	w := httptest.NewRecorder()
	body := map[string]string{"email": "sendcode-test@silieco.ai"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["message"] == "" {
		t.Fatal("SendCode: expected non-empty message")
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, "sendcode-test@silieco.ai")
	})
}

func TestSendCodeDbError(t *testing.T) {
	// We can't easily mock the DB here without changing architecture,
	// but we can simulate a DB error by closing the pool temporarily or
	// using a cancelled context if the query respects it.

	// Create a handler with a "broken" queries object is hard because it's a struct.
	// Instead, let's use a context that is already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := httptest.NewRecorder()
	body := map[string]string{"email": "dberror-test@silieco.ai"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)

	testHandler.SendCode(w, req)

	// If the DB query respects the cancelled context, it should return an error.
	// pgx usually returns context.Canceled which is not what isNotFound checks for.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("SendCode (db error): expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "failed to lookup user" {
		t.Fatalf("SendCode (db error): expected error message 'failed to lookup user', got '%s'", resp["error"])
	}
}

func TestSendCodeRateLimit(t *testing.T) {
	const email = "ratelimit-test@silieco.ai"
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// First request should succeed
	w := httptest.NewRecorder()
	body := map[string]string{"email": email}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode (first): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Second request within 60s should be rate limited
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(body)
	req = httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("SendCode (second): expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCode(t *testing.T) {
	const email = "verify-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		user, err := testHandler.Queries.GetUserByEmail(ctx, email)
		if err == nil {
			workspaces, listErr := testHandler.Queries.ListWorkspaces(ctx, user.ID)
			if listErr == nil {
				for _, workspace := range workspaces {
					_ = testHandler.Queries.DeleteWorkspace(ctx, workspace.ID)
				}
			}
		}
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Send code first
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read code from DB
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify with correct code
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp LoginResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Token == "" {
		t.Fatal("VerifyCode: expected non-empty token")
	}
	if resp.User.Email != email {
		t.Fatalf("VerifyCode: expected email '%s', got '%s'", email, resp.User.Email)
	}
}

func createVerificationCodeForTest(t *testing.T, email, code string) {
	t.Helper()

	_, err := testPool.Exec(context.Background(), `
		INSERT INTO verification_code (email, code, expires_at)
		VALUES ($1, $2, now() + interval '10 minutes')
	`, email, code)
	if err != nil {
		t.Fatalf("create verification code: %v", err)
	}
}

func TestVerifyCodeRejectsDevCodeUnlessExplicitlyConfigured(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "")
	t.Setenv("APP_ENV", "")

	const email = "dev-code-disabled-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (disabled dev code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeAcceptsConfiguredDevCodeOutsideProduction(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "888888")
	t.Setenv("APP_ENV", "development")

	const email = "dev-code-enabled-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode (enabled dev code): expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeRejectsConfiguredDevCodeInProduction(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "888888")
	t.Setenv("APP_ENV", "production")

	const email = "dev-code-production-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	createVerificationCodeForTest(t, email, "123456")

	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "888888"})
	req := httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (production dev code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeWrongCode(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "")

	const email = "wrong-code-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)

	// Verify with wrong code
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "000000"})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("VerifyCode (wrong code): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeBruteForceProtection(t *testing.T) {
	t.Setenv(devVerificationCodeEnv, "")

	const email = "bruteforce-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SendCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read actual code so we can try it after lockout
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Exhaust all 5 attempts with wrong codes
	for i := 0; i < 5; i++ {
		w = httptest.NewRecorder()
		buf.Reset()
		json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": "000000"})
		req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
		req.Header.Set("Content-Type", "application/json")
		testHandler.VerifyCode(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400, got %d", i+1, w.Code)
		}
	}

	// Now even the correct code should be rejected (code is locked out)
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("after lockout: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyCodeNewUserHasNoWorkspace(t *testing.T) {
	const email = "workspace-verify-test@silieco.ai"
	ctx := context.Background()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM verification_code WHERE email = $1`, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})

	// Send code
	w := httptest.NewRecorder()
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": email})
	req := httptest.NewRequest("POST", "/auth/send-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.SendCode(w, req)

	// Read code from DB
	dbCode, err := testHandler.Queries.GetLatestVerificationCode(ctx, email)
	if err != nil {
		t.Fatalf("GetLatestVerificationCode: %v", err)
	}

	// Verify
	w = httptest.NewRecorder()
	buf.Reset()
	json.NewEncoder(&buf).Encode(map[string]string{"email": email, "code": dbCode.Code})
	req = httptest.NewRequest("POST", "/auth/verify-code", &buf)
	req.Header.Set("Content-Type", "application/json")
	testHandler.VerifyCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VerifyCode: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	user, err := testHandler.Queries.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}

	// New users should have no workspaces (/workspaces/new creates one)
	workspaces, err := testHandler.Queries.ListWorkspaces(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("ListWorkspaces: expected 0 workspaces for new user, got %d", len(workspaces))
	}
}

func TestResolveActor(t *testing.T) {
	ctx := context.Background()

	// Look up the agent created by the test fixture.
	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create a task for the agent so we can test X-Run-ID validation.
	var taskID string
	err = testPool.QueryRow(ctx,
		`INSERT INTO task (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		 VALUES ($1, 'resolveActor test', 'todo', 'none', 'member', $2, 9999, 0)
		 RETURNING id`, testWorkspaceID, testUserID,
	).Scan(&taskID)
	if err != nil {
		t.Fatalf("failed to create test task: %v", err)
	}

	// Look up runtime_id for the agent.
	var runtimeID string
	err = testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("failed to get agent runtime_id: %v", err)
	}

	var runID string
	err = testPool.QueryRow(ctx,
		`INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority)
		 VALUES ($1, $2, $3, 'queued', 0)
		 RETURNING id`, agentID, runtimeID, taskID,
	).Scan(&runID)
	if err != nil {
		t.Fatalf("failed to create test run: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE id = $1`, runID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	tests := []struct {
		name          string
		agentIDHeader string
		runIDHeader   string
		wantActorType string
		wantIsAgent   bool
	}{
		{
			name:          "no headers returns member",
			wantActorType: "member",
		},
		{
			// X-Agent-ID without X-Run-ID is not trusted — otherwise a
			// workspace member who guesses an agent's UUID could impersonate
			// it and bypass the private-agent gate. See resolveActor for the
			// rationale.
			name:          "agent ID without task ID returns member",
			agentIDHeader: agentID,
			wantActorType: "member",
		},
		{
			name:          "non-existent agent ID with task returns member",
			agentIDHeader: "00000000-0000-0000-0000-000000000099",
			runIDHeader:   runID,
			wantActorType: "member",
		},
		{
			name:          "valid agent + valid task returns agent",
			agentIDHeader: agentID,
			runIDHeader:   runID,
			wantActorType: "agent",
			wantIsAgent:   true,
		},
		{
			name:          "valid agent + wrong task returns member",
			agentIDHeader: agentID,
			runIDHeader:   "00000000-0000-0000-0000-000000000099",
			wantActorType: "member",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest("GET", "/test", nil)
			if tt.agentIDHeader != "" {
				req.Header.Set("X-Agent-ID", tt.agentIDHeader)
			}
			if tt.runIDHeader != "" {
				req.Header.Set("X-Run-ID", tt.runIDHeader)
			}

			actorType, actorID := testHandler.resolveActor(req, testUserID, testWorkspaceID)

			if actorType != tt.wantActorType {
				t.Errorf("actorType = %q, want %q", actorType, tt.wantActorType)
			}
			if tt.wantIsAgent {
				if actorID != tt.agentIDHeader {
					t.Errorf("actorID = %q, want agent %q", actorID, tt.agentIDHeader)
				}
			} else {
				if actorID != testUserID {
					t.Errorf("actorID = %q, want user %q", actorID, testUserID)
				}
			}
		})
	}
}

// TestBacklogNoTriggerOnCreate verifies that creating a backlog task with an
// agent assignee does NOT enqueue a task — backlog is a parking lot.
func TestBacklogNoTriggerOnCreate(t *testing.T) {
	ctx := context.Background()

	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Backlog no-trigger test",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)

	var runCount int
	err = testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1`,
		created.ID,
	).Scan(&runCount)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("expected no tasks for backlog task on creation, got %d", runCount)
	}

	// Cleanup
	cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
}

// TestBacklogToTodoTriggersAgent verifies that moving an agent-assigned task
// from "backlog" to "todo" enqueues exactly one agent task (none on creation,
// one on status transition).
func TestBacklogToTodoTriggersAgent(t *testing.T) {
	ctx := context.Background()

	var agentID string
	err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	// Create a backlog task assigned to the agent — should NOT trigger.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Backlog trigger test",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Move the task from backlog to todo — should trigger.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{
		"status": "todo",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify exactly one task was enqueued (from the status transition, not creation).
	var runCount int
	err = testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, agentID,
	).Scan(&runCount)
	if err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected exactly 1 task after backlog->todo transition, got %d", runCount)
	}

	// Cleanup
	testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
	cleanupReq := newRequest("DELETE", "/api/tasks/"+created.ID, nil)
	cleanupReq = withURLParam(cleanupReq, "id", created.ID)
	testHandler.DeleteTask(httptest.NewRecorder(), cleanupReq)
}

// TestBacklogToTodoByAgentTriggersDifferentAssignee verifies that the
// documented sub-task chain works: when an agent (parent / Step 1) promotes
// a backlog task assigned to a different agent (child / Step 2), the
// child's task is enqueued. Previously the backlog→active trigger was
// gated on `actorType == "member"`, which silently dropped agent-driven
// promotions and broke the serial sub-task workflow.
func TestBacklogToTodoByAgentTriggersDifferentAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Parent agent (the actor) + child agent (the assignee).
	parentAgent := createHandlerTestAgent(t, "Backlog Parent Agent", nil)
	childAgent := createHandlerTestAgent(t, "Backlog Child Agent", nil)
	parentRun := createHandlerTestRunForAgent(t, parentAgent)

	// Create a backlog task assigned to the child agent — should NOT trigger
	// on creation (backlog parking-lot rule).
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Serial sub-task Step 2",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   childAgent,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, created.ID)
	})

	// Parent agent promotes backlog → todo on behalf of the X-Task it is
	// currently running. Must enqueue exactly one task for the child agent.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{"status": "todo"})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", parentAgent)
	req.Header.Set("X-Run-ID", parentRun)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var childRuns int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, childAgent,
	).Scan(&childRuns); err != nil {
		t.Fatalf("failed to count child tasks: %v", err)
	}
	if childRuns != 1 {
		t.Fatalf("expected exactly 1 task enqueued for child agent after agent-driven backlog→todo, got %d", childRuns)
	}
}

// TestBacklogToTodoByAgentSameTaskDoesNotSelfTrigger verifies the
// task-task-scoped self-loop guard: an agent whose CURRENT task is
// running on task I and who flips I from backlog to an active status
// must NOT enqueue itself for I again. Without this guard the agent
// would re-trigger every cycle it completed on I and immediately
// re-enter the same path.
//
// This is the true self-loop case (calling task is on the SAME task
// being promoted). The complementary case — same agent, DIFFERENT
// task — is the documented serial chain and is covered by
// TestBacklogToTodoByAgentSameAgentDifferentTask.
func TestBacklogToTodoByAgentSameTaskDoesNotSelfTrigger(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	selfAgent := createHandlerTestAgent(t, "Backlog Self Agent", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Self-promoted backlog",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   selfAgent,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, created.ID)
	})

	// Task bound to the SAME task being promoted — true self-loop.
	selfRun := createHandlerTestRunForAgentOnTask(t, selfAgent, created.ID)

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{"status": "todo"})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", selfAgent)
	req.Header.Set("X-Run-ID", selfRun)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agentRuns int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, selfAgent,
	).Scan(&agentRuns); err != nil {
		t.Fatalf("failed to count tasks: %v", err)
	}
	if agentRuns != 0 {
		t.Fatalf("expected no self-trigger when agent promotes the same task its task is running on, got %d queued tasks", agentRuns)
	}
}

// TestBacklogToTodoByAgentSameAgentDifferentTask verifies the documented
// same-agent serial chain still fires: when an agent is running a task on
// task I1 and promotes a DIFFERENT backlog task I2 (also assigned to
// itself), I2 must be enqueued. This was over-blocked by the previous
// agent-id-based self-loop guard, which made the same-agent serial
// workflow silently break.
func TestBacklogToTodoByAgentSameAgentDifferentTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "Backlog Same-Agent Chain", nil)

	// Step 1 task — the one the agent is currently working on.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Step 1 (running)",
		"status":        "in_progress",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask step1: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var step1 TaskResponse
	json.NewDecoder(w.Body).Decode(&step1)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, step1.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, step1.ID)
	})

	// Step 2 task — backlog, also assigned to the same agent.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Step 2 (backlog)",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask step2: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var step2 TaskResponse
	json.NewDecoder(w.Body).Decode(&step2)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, step2.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, step2.ID)
	})

	// Task is running on step1 — promoting step2 is NOT a self-loop.
	step1Run := createHandlerTestRunForAgentOnTask(t, agentID, step1.ID)

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+step2.ID, map[string]any{"status": "todo"})
	req = withURLParam(req, "id", step2.ID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Run-ID", step1Run)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask step2: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var step2Runs int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		step2.ID, agentID,
	).Scan(&step2Runs); err != nil {
		t.Fatalf("failed to count step2 tasks: %v", err)
	}
	if step2Runs != 1 {
		t.Fatalf("expected exactly 1 task enqueued on step2 for same-agent serial chain, got %d", step2Runs)
	}
}

// TestBatchBacklogToTodoByAgentTriggersAssignee mirrors the single-update
// serial-chain test on the BatchUpdateTasks path. Earlier the
// member-only gate would silently drop agent-driven batch promotions; the
// task-task self-loop guard must let cross-task (same-agent) batch
// promotions through.
func TestBatchBacklogToTodoByAgentTriggersAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	parentAgent := createHandlerTestAgent(t, "Batch Parent Agent", nil)
	childAgent := createHandlerTestAgent(t, "Batch Child Agent", nil)
	parentRun := createHandlerTestRunForAgent(t, parentAgent)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Batch backlog child",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   childAgent,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, created.ID)
	})

	// Drive the batch endpoint with the same agent identity headers.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/tasks/batch?workspace_id="+testWorkspaceID, map[string]any{
		"task_ids": []string{created.ID},
		"updates":  map[string]any{"status": "todo"},
	})
	req.Header.Set("X-Agent-ID", parentAgent)
	req.Header.Set("X-Run-ID", parentRun)
	testHandler.BatchUpdateTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var childRuns int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, childAgent,
	).Scan(&childRuns); err != nil {
		t.Fatalf("failed to count child tasks: %v", err)
	}
	if childRuns != 1 {
		t.Fatalf("expected exactly 1 task enqueued for child agent after batch agent-driven backlog→todo, got %d", childRuns)
	}
}

// TestBacklogToTodoByAgentTriggersSquadLeader covers the squad branch of
// the backlog→active trigger when the actor is an agent: the leader agent
// of a squad must wake when one of its squad-assigned backlog tasks is
// promoted by another agent (or by the leader itself acting from a task
// on a different task). The task-task self-loop guard must allow this —
// only a true same-task self-loop should be suppressed.
func TestBacklogToTodoByAgentTriggersSquadLeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderAgent := createHandlerTestAgent(t, "Backlog Squad Leader", nil)
	driverAgent := createHandlerTestAgent(t, "Backlog Squad Driver", nil)
	driverRun := createHandlerTestRunForAgent(t, driverAgent)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Backlog Trigger Squad", leaderAgent, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Squad backlog task",
		"status":        "backlog",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created TaskResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, created.ID)
	})

	// Driver agent (not the leader, task is on no specific task) promotes
	// the squad-assigned backlog task. Squad leader must be enqueued.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/tasks/"+created.ID, map[string]any{"status": "todo"})
	req = withURLParam(req, "id", created.ID)
	req.Header.Set("X-Agent-ID", driverAgent)
	req.Header.Set("X-Run-ID", driverRun)
	testHandler.UpdateTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var leaderRuns int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		created.ID, leaderAgent,
	).Scan(&leaderRuns); err != nil {
		t.Fatalf("failed to count leader tasks: %v", err)
	}
	if leaderRuns != 1 {
		t.Fatalf("expected exactly 1 squad-leader task after agent-driven backlog→todo on squad task, got %d", leaderRuns)
	}
}

func TestDaemonRegisterMissingWorkspaceReturns404(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/daemon/register", bytes.NewBufferString(`{
		"workspace_id":"00000000-0000-0000-0000-000000000001",
		"daemon_id":"local-daemon",
		"device_name":"test-machine",
		"runtimes":[{"name":"Local Codex","type":"codex","version":"1.0.0","status":"online"}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)

	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DaemonRegister: expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "workspace not found") {
		t.Fatalf("DaemonRegister: expected workspace not found error, got %s", w.Body.String())
	}
}

// TestRootMentionOwnerRoutesMemberReplyButNotAgentReply verifies that a member
// root @mention owns later member replies, while agent-authored acknowledgments
// still do not self-expand the route.
func TestRootMentionOwnerRoutesMemberReplyButNotAgentReply(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create two agents.
	agentA := createHandlerTestAgent(t, "Loop Agent A", nil)
	agentB := createHandlerTestAgent(t, "Loop Agent B", nil)

	// Create an unassigned task so on_comment doesn't fire.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Agent mention inheritance test",
		"status": "todo",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	taskID := task.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Helper: count queued tasks for a given agent on this task.
	countRuns := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
			taskID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// Helper: cancel all tasks for an agent on this task.
	cancelRuns := func(agentID string) {
		_, err := testPool.Exec(ctx,
			`UPDATE agent_run SET status = 'cancelled' WHERE task_id = $1 AND agent_id = $2`,
			taskID, agentID,
		)
		if err != nil {
			t.Fatalf("failed to cancel tasks: %v", err)
		}
	}

	postComment := func(taskID string, body map[string]any, headers map[string]string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		testHandler.CreateComment(w, r)
		return w
	}

	// 1. Member posts top-level comment mentioning Agent B.
	mentionB := fmt.Sprintf("[@Agent B](mention://agent/%s) please review", agentB)
	w = postComment(taskID, map[string]any{"content": mentionB}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("member mention comment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parentComment CommentResponse
	json.NewDecoder(w.Body).Decode(&parentComment)
	if countRuns(agentB) != 1 {
		t.Fatalf("expected 1 task for Agent B after member mention, got %d", countRuns(agentB))
	}

	// 2. Cancel Agent B's task so it's free to be re-triggered.
	cancelRuns(agentB)
	if countRuns(agentB) != 0 {
		t.Fatalf("expected 0 tasks for Agent B after cancel, got %d", countRuns(agentB))
	}

	// 3. Agent A posts a reply in the same thread with NO mentions.
	// Agent-authored comments still do not inherit the root mention of Agent B.
	// resolveActor requires X-Run-ID paired with X-Agent-ID to trust the
	// agent identity, so we seed a task that belongs to agent A.
	agentARun := createHandlerTestRunForAgent(t, agentA)
	w = postComment(taskID, map[string]any{
		"content":   "No reply needed — just an acknowledgment.",
		"parent_id": parentComment.ID,
	}, map[string]string{"X-Agent-ID": agentA, "X-Run-ID": agentARun})
	if w.Code != http.StatusCreated {
		t.Fatalf("agent A reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if countRuns(agentB) != 0 {
		t.Fatalf("expected 0 tasks for Agent B after agent reply (no parent inheritance), got %d", countRuns(agentB))
	}

	// 4. Cancel any stray tasks.
	cancelRuns(agentB)

	// 5. Member posts a reply in the same thread with NO mentions.
	// The member-authored root @mention owns the thread, so this routes to B.
	w = postComment(taskID, map[string]any{
		"content":   "Thanks for the review.",
		"parent_id": parentComment.ID,
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("member reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if countRuns(agentB) != 1 {
		t.Fatalf("expected 1 task for Agent B after member reply (root owner), got %d", countRuns(agentB))
	}
}

// TestMemberReplyToAgentRootDoesNotInheritParentMentions is the regression
// for SILI-1535. When an agent posts a comment that @mentions another agent
// (e.g. J posting a PR completion that @mentions a reviewer agent), a later
// member reply in the same thread with no explicit mentions must NOT inherit
// the @reviewer mention. The reviewer was a one-shot delegation; subsequent
// member follow-ups are directed at the assignee, not the reviewer.
func TestMemberReplyToAgentRootDoesNotInheritParentMentions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	jAgent := createHandlerTestAgent(t, "J", nil)
	reviewerAgent := createHandlerTestAgent(t, "Reviewer", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "PR review delegation no-leak test",
		"status": "todo",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	taskID := task.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	countRuns := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
			taskID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// 1. Agent J posts a PR-completion comment that @mentions Reviewer for review.
	// This is a deliberate handoff and must enqueue a task for Reviewer.
	// X-Run-ID is required alongside X-Agent-ID for resolveActor to grant
	// the "agent" actor identity (defense against header forgery).
	jAgentRun := createHandlerTestRunForAgent(t, jAgent)
	w = httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": fmt.Sprintf("PR ready. [@Reviewer](mention://agent/%s) please review this.", reviewerAgent),
	})
	r = withURLParam(r, "id", taskID)
	r.Header.Set("X-Agent-ID", jAgent)
	r.Header.Set("X-Run-ID", jAgentRun)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("J PR completion: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var rootComment CommentResponse
	json.NewDecoder(w.Body).Decode(&rootComment)
	if got := countRuns(reviewerAgent); got != 1 {
		t.Fatalf("expected 1 task for Reviewer after explicit mention, got %d", got)
	}

	// Cancel reviewer's task so it's free to be re-triggered if the bug returns.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_run SET status = 'cancelled' WHERE task_id = $1 AND agent_id = $2`,
		taskID, reviewerAgent,
	); err != nil {
		t.Fatalf("cancel reviewer run: %v", err)
	}

	// 2. Member posts a plain follow-up reply under J's PR comment, with no
	// explicit mentions. The pre-fix code path inherited mentions from the
	// parent regardless of the parent author, which re-triggered Reviewer.
	// With the fix, the reply must NOT inherit because the parent was
	// authored by an agent.
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content":   "How do I test this after merging?",
		"parent_id": rootComment.ID,
	})
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("member follow-up: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := countRuns(reviewerAgent); got != 0 {
		t.Fatalf("expected 0 tasks for Reviewer after plain member reply (no inheritance from agent root), got %d", got)
	}
}

// TestNestedMemberReplyUsesDirectParentForThreadOwnership is the regression
// for parent-root write normalization leaking root mentions into plain nested
// replies. Stored parent_id keeps the direct parent, and trigger logic routes
// to that direct agent parent rather than the thread root's explicit mention.
func TestNestedMemberReplyUsesDirectParentForMentionInheritance(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeAgent := createHandlerTestAgent(t, "Nested Mention Assignee", nil)
	mentionedAgent := createHandlerTestAgent(t, "Nested Mention Target", nil)
	parentAgent := createHandlerTestAgent(t, "Nested Direct Parent", nil)

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(task_counter, (SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING task_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next task number: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'member', $2, $3, 'agent', $4, $5)
		RETURNING id
	`, testWorkspaceID, testUserID, "nested mention inheritance regression", assigneeAgent, number).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	countQueued := func(agentID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_run
			WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
		`, taskID, agentID).Scan(&n); err != nil {
			t.Fatalf("count queued tasks: %v", err)
		}
		return n
	}
	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment response: %v", err)
		}
		return resp
	}

	root := postMemberComment(map[string]any{
		"content": fmt.Sprintf("[@Mentioned](mention://agent/%s) please look", mentionedAgent),
	})
	if got := countQueued(mentionedAgent); got != 1 {
		t.Fatalf("expected root mention to queue mentioned agent once, got %d", got)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET status = 'cancelled' WHERE task_id = $1`, taskID); err != nil {
		t.Fatalf("cancel root mention run: %v", err)
	}

	var directParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content, parent_id)
		VALUES ($1, $2, 'agent', $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, taskID, parentAgent, "looks like redirect config", root.ID).Scan(&directParentID); err != nil {
		t.Fatalf("insert direct parent reply: %v", err)
	}

	nested := postMemberComment(map[string]any{
		"content":   "can you also check session expiry?",
		"parent_id": directParentID,
	})
	if nested.ParentID == nil || *nested.ParentID != directParentID {
		t.Fatalf("stored nested reply parent_id should keep direct parent %s, got %v", directParentID, nested.ParentID)
	}
	if got := countQueued(mentionedAgent); got != 0 {
		t.Fatalf("plain nested reply must not inherit root mention from non-direct parent; got %d queued tasks", got)
	}
	if got := countQueued(parentAgent); got != 1 {
		t.Fatalf("plain nested reply should route to direct agent parent; got %d queued tasks", got)
	}
}

// TestNestedMemberReplyWithMemberParentFallsBackToAssignee verifies that a
// nested reply whose direct parent is human-owned does not route to a sibling
// agent reply. It falls through to the task assignee instead.
func TestNestedMemberReplyUsesDirectParentForAssigneeParticipation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeAgent := createHandlerTestAgent(t, "Nested Participation Assignee", nil)

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(task_counter, (SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING task_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next task number: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'member', $2, $3, 'agent', $4, $5)
		RETURNING id
	`, testWorkspaceID, testUserID, "nested participation regression", assigneeAgent, number).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	countAssigneeQueued := func() int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(ctx, `
			SELECT count(*) FROM agent_run
			WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
		`, taskID, assigneeAgent).Scan(&n); err != nil {
			t.Fatalf("count queued tasks: %v", err)
		}
		return n
	}
	postMemberComment := func(body map[string]any) CommentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest("POST", "/api/tasks/"+taskID+"/comments", body)
		r = withURLParam(r, "id", taskID)
		testHandler.CreateComment(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp CommentResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode comment response: %v", err)
		}
		return resp
	}

	var rootID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'this cache question is for humans')
		RETURNING id
	`, testWorkspaceID, taskID, testUserID).Scan(&rootID); err != nil {
		t.Fatalf("insert root comment: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content, parent_id)
		VALUES ($1, $2, 'agent', $3, 'expiration policy is the task', $4)
	`, testWorkspaceID, taskID, assigneeAgent, rootID); err != nil {
		t.Fatalf("insert assignee reply: %v", err)
	}
	var humanParentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content, parent_id)
		VALUES ($1, $2, 'member', $3, 'I have seen this too', $4)
		RETURNING id
	`, testWorkspaceID, taskID, testUserID, rootID).Scan(&humanParentID); err != nil {
		t.Fatalf("insert human direct parent: %v", err)
	}

	nested := postMemberComment(map[string]any{
		"content":   "what should the expiration be?",
		"parent_id": humanParentID,
	})
	if nested.ParentID == nil || *nested.ParentID != humanParentID {
		t.Fatalf("stored nested reply parent_id should keep direct parent %s, got %v", humanParentID, nested.ParentID)
	}
	if got := countAssigneeQueued(); got != 1 {
		t.Fatalf("plain nested human reply should fall back to assignee; got %d queued tasks", got)
	}
}

// TestAgentExplicitMentionStillTriggers documents the boundary the structural
// fix preserves: suppressing implicit parent-mention inheritance for agent
// authors does NOT block deliberate handoffs. An agent that explicitly
// @mentions another agent in its own comment content still enqueues a task
// for that mentioned agent.
func TestAgentExplicitMentionStillTriggers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentA := createHandlerTestAgent(t, "Handoff Agent A", nil)
	agentB := createHandlerTestAgent(t, "Handoff Agent B", nil)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Agent explicit handoff test",
		"status": "todo",
	})
	testHandler.CreateTask(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var task TaskResponse
	json.NewDecoder(w.Body).Decode(&task)
	taskID := task.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM task WHERE id = $1`, taskID)
	})

	countRuns := func(agentID string) int {
		var n int
		err := testPool.QueryRow(ctx,
			`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
			taskID, agentID,
		).Scan(&n)
		if err != nil {
			t.Fatalf("failed to count tasks: %v", err)
		}
		return n
	}

	// Agent A posts a top-level comment that explicitly @mentions Agent B —
	// a deliberate handoff. This must enqueue a task for Agent B, and must
	// not enqueue a self-trigger for Agent A. resolveActor requires
	// X-Run-ID to grant "agent" identity; without it the self-trigger
	// suppression (authorType=="agent") would not fire.
	agentARun := createHandlerTestRunForAgent(t, agentA)
	explicitMention := fmt.Sprintf("[@Agent B](mention://agent/%s) please take it from here", agentB)
	w = httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": explicitMention,
	})
	r = withURLParam(r, "id", taskID)
	r.Header.Set("X-Agent-ID", agentA)
	r.Header.Set("X-Run-ID", agentARun)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent A handoff: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if got := countRuns(agentB); got != 1 {
		t.Fatalf("expected 1 task for Agent B after explicit mention by Agent A, got %d", got)
	}
	if got := countRuns(agentA); got != 0 {
		t.Fatalf("expected 0 tasks for Agent A (no self-trigger on own mention), got %d", got)
	}
}

func TestCreateSkillSkipsSkillMdFile(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
		Name:    "test-skill-create-skip-skillmd",
		Content: "# SKILL.md content",
		Files: []CreateSkillFileRequest{
			{Path: "README.md", Content: "readme"},
			{Path: "SKILL.md", Content: "should be skipped"},
			{Path: "helper.go", Content: "package main"},
		},
	})
	rec := httptest.NewRecorder()
	testHandler.CreateSkill(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SkillWithFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Should only have README.md and helper.go, not SKILL.md
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}
	for _, f := range resp.Files {
		if strings.EqualFold(f.Path, "SKILL.md") {
			t.Fatalf("SKILL.md should not be in response files")
		}
	}

	// Verify DB state directly
	ctx := context.Background()
	rows, err := testPool.Query(ctx, "SELECT path FROM skill_file WHERE skill_id = $1", resp.ID)
	if err != nil {
		t.Fatalf("query skill_file: %v", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		paths = append(paths, p)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 rows in skill_file, got %d", len(paths))
	}
	for _, p := range paths {
		if strings.EqualFold(p, "SKILL.md") {
			t.Fatalf("SKILL.md should not be stored in skill_file")
		}
	}
}

func TestUpdateSkillSkipsSkillMdFile(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	// Create a skill first
	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
		Name:    "test-skill-update-skip-skillmd",
		Content: "# SKILL.md content",
		Files: []CreateSkillFileRequest{
			{Path: "README.md", Content: "readme"},
		},
	})
	rec := httptest.NewRecorder()
	testHandler.CreateSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp SkillWithFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	// Update with SKILL.md in files
	updateReq := newRequest(http.MethodPut, "/api/skills/"+createResp.ID, UpdateSkillRequest{
		Name:    strPtr("updated-name"),
		Content: strPtr("updated content"),
		Files: []CreateSkillFileRequest{
			{Path: "README.md", Content: "updated readme"},
			{Path: "SKILL.md", Content: "should be skipped"},
			{Path: "new.go", Content: "package main"},
		},
	})
	updateReq = withURLParam(updateReq, "id", createResp.ID)
	updateRec := httptest.NewRecorder()
	testHandler.UpdateSkill(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("update skill: expected 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}

	var updateResp SkillWithFilesResponse
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("unmarshal update response: %v", err)
	}

	if len(updateResp.Files) != 2 {
		t.Fatalf("expected 2 files after update, got %d", len(updateResp.Files))
	}
	for _, f := range updateResp.Files {
		if strings.EqualFold(f.Path, "SKILL.md") {
			t.Fatalf("SKILL.md should not be in updated response files")
		}
	}

	// Verify DB state
	ctx := context.Background()
	rows, err := testPool.Query(ctx, "SELECT path FROM skill_file WHERE skill_id = $1", createResp.ID)
	if err != nil {
		t.Fatalf("query skill_file: %v", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan path: %v", err)
		}
		paths = append(paths, p)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 rows in skill_file after update, got %d", len(paths))
	}
	for _, p := range paths {
		if strings.EqualFold(p, "SKILL.md") {
			t.Fatalf("SKILL.md should not be stored in skill_file after update")
		}
	}
}

func strPtr(s string) *string {
	return &s
}

func TestUpsertSkillFileRejectsSkillMd(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	// Create a skill first
	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
		Name:    "test-skill-upsert-reject-skillmd",
		Content: "# SKILL.md content",
	})
	rec := httptest.NewRecorder()
	testHandler.CreateSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp SkillWithFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}

	// Try to upsert SKILL.md
	upsertReq := newRequest(http.MethodPut, "/api/skills/"+createResp.ID+"/files", CreateSkillFileRequest{
		Path:    "SKILL.md",
		Content: "should be rejected",
	})
	upsertReq = withURLParam(upsertReq, "id", createResp.ID)
	upsertRec := httptest.NewRecorder()
	testHandler.UpsertSkillFile(upsertRec, upsertReq)

	if upsertRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", upsertRec.Code, upsertRec.Body.String())
	}
	if !strings.Contains(upsertRec.Body.String(), "SKILL.md is reserved") {
		t.Fatalf("expected error message about reserved SKILL.md, got: %s", upsertRec.Body.String())
	}
}
