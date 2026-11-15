package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// CancelTaskByUser (POST /api/tasks/{runId}/cancel) used to key cancellation
// off task_id / chat_session_id alone, which 404'd every task whose only
// source link was autopilot_run_id or quick_create context (SILI-2827). These
// tests pin the new behavior: tenancy flows through the task's owning agent,
// with chat-creator privacy and the private-agent visibility gate layered on.

// taskStatus reads a task's current status straight from the DB so reject
// paths can assert "no side effect before the access check".
func runStatus(t *testing.T, runID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_run WHERE id = $1`, runID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

// createAutopilotRunOnlyTask seeds the autopilot -> autopilot_run -> task chain
// that AutopilotService.dispatchRunOnly produces: a queued task with task_id
// and chat_session_id NULL, linked only by autopilot_run_id. The autopilot is
// created in the agent's own workspace so the fixture works for foreign agents
// too.
func createAutopilotRunOnlyRun(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID, runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT workspace_id, runtime_id FROM agent WHERE id = $1`, agentID,
	).Scan(&workspaceID, &runtimeID); err != nil {
		t.Fatalf("load agent workspace/runtime: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'cancel-runonly-ap', $2, 'run_only', 'member', $3)
		RETURNING id
	`, workspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	var autopilotRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status)
		VALUES ($1, 'manual', 'running')
		RETURNING id
	`, autopilotID).Scan(&autopilotRunID); err != nil {
		t.Fatalf("create autopilot_run: %v", err)
	}

	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, autopilot_run_id)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id
	`, agentID, runtimeID, autopilotRunID).Scan(&runID); err != nil {
		t.Fatalf("create run_only run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })
	return runID
}

// createForeignWorkspaceAgent stands up an isolated workspace + runtime + agent
// and returns the agent ID, for cross-tenant cancel tests.
func createForeignWorkspaceAgent(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, task_prefix)
		VALUES ('Foreign Cancel WS', 'foreign-cancel-ws', 'cross-tenant cancel test', 'FCW')
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'Foreign Cancel Runtime', 'cloud', 'foreign_runtime', 'online', 'Foreign runtime', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_runs)
		VALUES ($1, 'Foreign Cancel Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}
	return agentID
}

// createWorkspaceMemberUser adds a plain (non-owner/admin) member to the test
// workspace and returns the user ID. The member row cascades when the user is
// deleted (member.user_id ON DELETE CASCADE).
func createWorkspaceMemberUser(t *testing.T, name, email string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add member %s: %v", email, err)
	}
	return userID
}

func cancelRunByUserRequest(t *testing.T, userID, runID string) *http.Request {
	t.Helper()
	req := newRequestAs(userID, "POST", "/api/runs/"+runID+"/cancel", nil)
	req = withURLParam(req, "runId", runID)
	return withChatTestWorkspaceCtx(t, req)
}

// createStartedEmptyChatTask seeds the exact shape the deferred cancel is about:
// a chat task the daemon has already started (started_at set) whose transcript is
// still empty, plus the user message that triggered it.
func createStartedEmptyChatRun(t *testing.T, sessionID, agentID, content string) (runID, userMessageID string) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, chat_session_id, started_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now())
		RETURNING id
	`, agentID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create started chat run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, run_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, content, runID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	return runID, userMessageID
}

func chatFinalizeDeferredAt(t *testing.T, runID string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := testPool.QueryRow(context.Background(), `
		SELECT chat_finalize_deferred_at FROM agent_run WHERE id = $1
	`, runID).Scan(&at); err != nil {
		t.Fatalf("read chat_finalize_deferred_at: %v", err)
	}
	return at
}

// Rollout compatibility (#5219). Clients and server do not upgrade together, and
// a started-but-empty cancel is the one case where the prompt does NOT come back
// in the cancel response — it is deferred and later published as a durable
// draft-restore row, which only a client that knows about chat:cancel_finalized
// and the draft-restores endpoint can collect. An installed desktop build that
// predates all of that would read the empty response as "nothing to restore" and
// silently drop the user's input, so deferral is gated on the client advertising
// AppCapabilityChatDraftRestoreV1.
func TestCancelRunByUser_StartedEmptyChat_WithDraftRestoreCapability_Defers(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferCapableAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	runID, userMessageID := createStartedEmptyChatRun(t, sessionID, agentID, "defer this prompt")

	req := cancelRunByUserRequest(t, testUserID, runID)
	req.Header.Set("X-Client-Capabilities", protocol.AppCapabilityChatDraftRestoreV1)
	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelRunByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("capable client must get no synchronous restore, got %#v", resp.CancelledChatMessage)
	}
	if chatFinalizeDeferredAt(t, runID) == nil {
		t.Fatal("expected the finalize-deferred marker to be set")
	}

	// The judgment is deferred, so the user message must still be there for the
	// daemon ack / sweeper to settle.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the user message to survive the deferral, got %d rows", count)
	}
}

func TestCancelRunByUser_StartedEmptyChat_LegacyClient_StillGetsSynchronousRestore(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatDeferLegacyAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)
	const userContent = "an old client must not lose this"
	runID, userMessageID := createStartedEmptyChatRun(t, sessionID, agentID, userContent)

	// No X-Client-Capabilities: a build that predates the durable restore.
	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelRunByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("legacy client must still get the synchronous restore payload")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}
	if at := chatFinalizeDeferredAt(t, runID); at != nil {
		t.Fatalf("legacy cancel must not defer, marker set at %v", at)
	}

	// Legacy semantics all the way: the message is settled synchronously, and no
	// durable restore row is written for a client that could never read it.
	var messages int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID).Scan(&messages); err != nil {
		t.Fatalf("count user chat message: %v", err)
	}
	if messages != 0 {
		t.Fatalf("expected the user message to be deleted synchronously, got %d rows", messages)
	}
	var restores int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_draft_restore WHERE chat_session_id = $1`, sessionID).Scan(&restores); err != nil {
		t.Fatalf("count draft restores: %v", err)
	}
	if restores != 0 {
		t.Fatalf("expected no durable restore for a legacy cancel, got %d", restores)
	}
}

// TestCancelTaskByUser_RunOnlyAutopilot_Succeeds is the core SILI-2827 fix: a
// run_only autopilot task (task_id + chat_session_id NULL, only
// autopilot_run_id set) is cancellable by a member of its agent's workspace.
func TestCancelRunByUser_RunOnlyAutopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRunOnlyAgent", []byte("[]"))
	runID := createAutopilotRunOnlyRun(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_RunOnlyAutopilot_CrossWorkspace_Returns404 verifies the
// tenant guard: a member of workspace A cannot cancel a run_only task whose
// agent lives in workspace B, and the task is not mutated before the check.
func TestCancelRunByUser_RunOnlyAutopilot_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignAgentID := createForeignWorkspaceAgent(t)
	runID := createAutopilotRunOnlyRun(t, foreignAgentID)

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "queued" {
		t.Fatalf("foreign task was mutated: status = %q", got)
	}
}

// TestCancelTaskByUser_QuickCreate_Succeeds verifies a quick_create task — no
// task yet, no chat session, only context JSONB — is cancellable during its
// active window (the pre-task-creation phase, i.e. whenever the user clicks X).
func TestCancelRunByUser_QuickCreate_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelQuickCreateAgent", []byte("[]"))

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, context)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL,
		        '{"type":"quick_create","workspace_id":"ws","prompt":"do a thing"}'::jsonb)
		RETURNING id
	`, agentID).Scan(&runID); err != nil {
		t.Fatalf("create quick_create run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_RetryClone_Autopilot_Succeeds verifies a retry clone of
// an autopilot task — which copies parent_run_id + autopilot_run_id verbatim,
// inheriting the NULL task/chat links — is still cancellable.
func TestCancelRunByUser_RetryClone_Autopilot_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelRetryCloneAgent", []byte("[]"))
	parentID := createAutopilotRunOnlyRun(t, agentID)

	var cloneID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, autopilot_run_id, parent_run_id, attempt)
		SELECT agent_id, runtime_id, 'queued', priority, autopilot_run_id, id, 1
		FROM agent_run WHERE id = $1
		RETURNING id
	`, parentID).Scan(&cloneID); err != nil {
		t.Fatalf("create retry clone: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, cloneID) })

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, cloneID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, cloneID); got != "cancelled" {
		t.Fatalf("clone not cancelled: status = %q", got)
	}
}

// TestCancelRunByUser_TaskRun_Succeeds is a regression guard: task-bound
// tasks (the original supported case) stay cancellable after the rewrite.
func TestCancelRunByUser_TaskRun_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelTaskTaskAgent", []byte("[]"))

	var taskID, runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO task (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-byid-task', 'todo', 'medium', $2, 'member', 92001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID) })

	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'queued', 0, $2)
		RETURNING id
	`, agentID, taskID).Scan(&runID); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}
}

// TestCancelTaskByUser_ChatTask_NonCreator_Returns403 preserves chat privacy:
// a workspace member who did not start the conversation cannot cancel its task.
func TestCancelRunByUser_ChatRun_NonCreator_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID) // creator = testUserID
	otherUserID := createWorkspaceMemberUser(t, "Chat Bystander", "cancel-chat-bystander@silieco.test")

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create chat run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, otherUserID, runID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "running" {
		t.Fatalf("chat task was mutated: status = %q", got)
	}
}

func TestCancelRunByUser_ChatRunWithTranscript_PersistsAssistantSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, chat_session_id, created_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2, now() - interval '5 seconds')
		RETURNING id
	`, agentID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create chat run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, run_id)
		VALUES ($1, 'user', 'please answer', $2)
	`, sessionID, runID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_run_message (run_id, seq, type, content)
		VALUES ($1, 1, 'text', 'partial answer')
	`, runID); err != nil {
		t.Fatalf("create task message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelRunByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage != nil {
		t.Fatalf("expected no restore payload when transcript exists, got %#v", resp.CancelledChatMessage)
	}
	if got := runStatus(t, runID); got != "cancelled" {
		t.Fatalf("task not cancelled: status = %q", got)
	}

	var role, content, messageRunID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT role, content, COALESCE(run_id::text, '')
		FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&role, &content, &messageRunID); err != nil {
		t.Fatalf("read cancelled assistant chat message: %v", err)
	}
	if role != "assistant" || content != "Stopped." || messageRunID != runID {
		t.Fatalf("assistant snapshot mismatch: role=%q content=%q run_id=%q", role, content, messageRunID)
	}
}

func TestCancelRunByUser_ChatRunWithoutTranscript_RestoresUserDraft(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatNoTranscriptAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create chat run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	var userMessageID string
	const userContent = "keep this prompt"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, run_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, runID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelRunByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}
	if resp.CancelledChatMessage.MessageID != userMessageID ||
		resp.CancelledChatMessage.Content != userContent ||
		!resp.CancelledChatMessage.RestoreToInput {
		t.Fatalf("restore payload mismatch: %#v", resp.CancelledChatMessage)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE chat_session_id = $1 AND role = 'assistant'
	`, sessionID).Scan(&count); err != nil {
		t.Fatalf("count assistant chat messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no assistant snapshot for empty transcript, got %d", count)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM chat_message
		WHERE id = $1
	`, userMessageID).Scan(&count); err != nil {
		t.Fatalf("count deleted user chat message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}
}

// TestCancelTaskByUser_ChatTaskWithBoundAttachment_SurvivesCancelAndRebinds
// guards the data-loss path on the empty-chat cancel: the user message bound to
// an attachment is deleted, and attachment.chat_message_id is ON DELETE CASCADE
// (core/migrations/083_attachment_chat_columns.up.sql), so without the
// detach-before-delete step the cancel would silently destroy the user's
// attachment. The detach (chat_message_id -> NULL, chat_session_id retained) is
// load-bearing, not an optimization; nothing else covered it. This pins:
//
//	(a) the attachment row survives the cascade — still present, chat_message_id
//	    NULL, chat_session_id retained;
//	(b) the cancel response returns it via cancelled_chat_message.attachments so
//	    the restored draft can re-show it;
//	(c) re-sending the restored draft re-binds the surviving attachment to the
//	    new message in the same session.
func TestCancelRunByUser_ChatRunWithBoundAttachment_SurvivesCancelAndRebinds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "CancelChatAttachAgent", []byte("[]"))
	sessionID := createHandlerTestChatSession(t, agentID)

	var runID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, chat_session_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, NULL, $2)
		RETURNING id
	`, agentID, sessionID).Scan(&runID); err != nil {
		t.Fatalf("create chat run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID) })

	var userMessageID string
	const userContent = "look at this attachment"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_message (chat_session_id, role, content, run_id)
		VALUES ($1, 'user', $2, $3)
		RETURNING id
	`, sessionID, userContent, runID).Scan(&userMessageID); err != nil {
		t.Fatalf("create linked user chat message: %v", err)
	}

	// Bind an attachment to that user message, exactly as a real send does:
	// workspace-scoped, uploaded by the session creator, pointing at both the
	// session and the message.
	var attachmentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, chat_session_id, chat_message_id)
		VALUES ($1, 'member', $2, 'cancel-survive.png', 'https://cdn.example.com/cancel-survive.png', 'image/png', 9, $3, $4)
		RETURNING id::text
	`, testWorkspaceID, testUserID, sessionID, userMessageID).Scan(&attachmentID); err != nil {
		t.Fatalf("seed bound attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	// Cancel the empty chat task (no transcript) — this deletes the user message.
	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, testUserID, runID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CancelRunByUserResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode cancel response: %v", err)
	}
	if resp.CancelledChatMessage == nil {
		t.Fatal("expected restore payload for empty transcript cancel")
	}

	// (b) The cancel response carries the detached attachment back.
	var returned *AttachmentResponse
	for i := range resp.CancelledChatMessage.Attachments {
		if resp.CancelledChatMessage.Attachments[i].ID == attachmentID {
			returned = &resp.CancelledChatMessage.Attachments[i]
			break
		}
	}
	if returned == nil {
		t.Fatalf("cancel response did not return the detached attachment: %#v", resp.CancelledChatMessage.Attachments)
	}

	// (a) The row survived the ON DELETE CASCADE: still present, detached from
	//     the deleted message, but still scoped to the session.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&count); err != nil {
		t.Fatalf("count attachment: %v", err)
	}
	if count != 1 {
		t.Fatalf("attachment was cascade-deleted on cancel: count = %d", count)
	}
	var dbMessageID, dbSessionID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text, chat_session_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID, &dbSessionID); err != nil {
		t.Fatalf("read attachment after cancel: %v", err)
	}
	if dbMessageID != nil {
		t.Fatalf("expected chat_message_id detached to NULL, got %q", *dbMessageID)
	}
	if dbSessionID == nil || *dbSessionID != sessionID {
		t.Fatalf("expected chat_session_id retained as %q, got %v", sessionID, dbSessionID)
	}

	// Sanity: the empty-cancel still deleted the user message itself.
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM chat_message WHERE id = $1`, userMessageID,
	).Scan(&count); err != nil {
		t.Fatalf("count user message: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected linked user message to be deleted, got %d", count)
	}

	// (c) Re-sending the restored draft re-binds the surviving attachment to a
	//     fresh message in the same session — the whole reason for detaching.
	sendReq := newRequest("POST", "/api/chat-sessions/"+sessionID+"/messages", map[string]any{
		"content":        userContent,
		"attachment_ids": []string{attachmentID},
	})
	sendReq = withURLParam(sendReq, "sessionId", sessionID)
	sendReq = withChatTestWorkspaceCtx(t, sendReq)
	sendW := httptest.NewRecorder()
	testHandler.SendChatMessage(sendW, sendReq)
	if sendW.Code != http.StatusCreated {
		t.Fatalf("resend: expected 201, got %d: %s", sendW.Code, sendW.Body.String())
	}
	var sendResp SendChatMessageResponse
	if err := json.Unmarshal(sendW.Body.Bytes(), &sendResp); err != nil {
		t.Fatalf("decode resend response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, sendResp.RunID)
	})

	rebound := false
	for _, id := range sendResp.AttachmentIDs {
		if id == attachmentID {
			rebound = true
			break
		}
	}
	if !rebound {
		t.Fatalf("attachment not re-bound on resend: %#v", sendResp.AttachmentIDs)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT chat_message_id::text FROM attachment WHERE id = $1`, attachmentID,
	).Scan(&dbMessageID); err != nil {
		t.Fatalf("read attachment after resend: %v", err)
	}
	if dbMessageID == nil || *dbMessageID != sendResp.MessageID {
		t.Fatalf("expected attachment re-bound to new message %q, got %v", sendResp.MessageID, dbMessageID)
	}
}

// TestCancelTaskByUser_PrivateAgent_PlainMember_Returns403 verifies the cancel
// endpoint mirrors the agent Activity / snapshot visibility gate: a plain
// member who cannot see a private agent's tasks cannot cancel them either.
func TestCancelRunByUser_PrivateAgent_PlainMember_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID, _, memberID := privateAgentTestFixture(t)
	runID := createAutopilotRunOnlyRun(t, agentID)

	w := httptest.NewRecorder()
	testHandler.CancelRunByUser(w, cancelRunByUserRequest(t, memberID, runID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := runStatus(t, runID); got != "queued" {
		t.Fatalf("task was mutated: status = %q", got)
	}
}
