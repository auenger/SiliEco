package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// authRequestWithAgent makes an authenticated request with X-Agent-ID +
// X-Run-ID headers, causing the server to resolve the actor as an agent
// instead of a member. resolveActor requires both headers to grant agent
// identity (defense against header forgery — see #2359 PR review), so we
// seed a queued task for the agent on demand and pass its UUID as
// X-Run-ID. The task is best-effort cleaned up via test teardown elsewhere.
func authRequestWithAgent(t *testing.T, method, path string, body any, agentID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, testServer.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Silieco-Protocol", "task-run-v1")
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Run-ID", ensureAgentRun(t, agentID))

	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return r
}

// ensureAgentTask returns a queued task UUID belonging to the given agent,
// inserting one if none exists. Used by authRequestWithAgent so callers
// can keep treating "set X-Agent-ID" as the single knob for posing as an
// agent — resolveActor's pair-required policy is satisfied transparently.
func ensureAgentRun(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()
	var runID string
	if err := testPool.QueryRow(ctx,
		`SELECT id::text FROM agent_run WHERE agent_id = $1 LIMIT 1`,
		agentID,
	).Scan(&runID); err == nil && runID != "" {
		return runID
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id::text FROM agent WHERE id = $1`,
		agentID,
	).Scan(&runtimeID); err != nil {
		t.Fatalf("ensureAgentTask: load runtime_id for agent %s: %v", agentID, err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'queued', 0)
		RETURNING id::text
	`, agentID, runtimeID).Scan(&runID); err != nil {
		t.Fatalf("ensureAgentTask: insert task for agent %s: %v", agentID, err)
	}
	return runID
}

// countPendingTasks returns the number of queued/dispatched tasks for an task.
func countPendingRuns(t *testing.T, taskID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND status IN ('queued', 'dispatched')`,
		taskID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pending tasks: %v", err)
	}
	return count
}

func countPendingRunsForAgent(t *testing.T, taskID, agentID string) int {
	t.Helper()
	var count int
	err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		taskID, agentID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count pending tasks for agent: %v", err)
	}
	return count
}

// clearTasks deletes all tasks for an task (cleanup between subtests).
func clearRuns(t *testing.T, taskID string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(),
		`DELETE FROM agent_run WHERE task_id = $1`, taskID)
	if err != nil {
		t.Fatalf("failed to clear tasks: %v", err)
	}
}

// latestTriggerCommentID returns the trigger_comment_id of the most recently
// created queued/dispatched task for the given task, or empty string if none.
func latestTriggerCommentID(t *testing.T, taskID string) string {
	t.Helper()
	var triggerID *string
	err := testPool.QueryRow(context.Background(),
		`SELECT trigger_comment_id::text
		   FROM agent_run
		  WHERE task_id = $1 AND status IN ('queued', 'dispatched')
		  ORDER BY created_at DESC
		  LIMIT 1`,
		taskID).Scan(&triggerID)
	if err != nil {
		t.Fatalf("failed to fetch trigger_comment_id: %v", err)
	}
	if triggerID == nil {
		return ""
	}
	return *triggerID
}

// getAgentID returns the ID of the first agent in the test workspace.
func getAgentID(t *testing.T) string {
	t.Helper()
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	return agents[0]["id"].(string)
}

// createSecondAgent creates a second agent in the test workspace and returns its ID.
// It reuses the same runtime as the first agent.
func createSecondAgent(t *testing.T) string {
	t.Helper()
	// Fetch the first agent to get its runtime_id.
	resp := authRequest(t, "GET", "/api/agents?workspace_id="+testWorkspaceID, nil)
	var agents []map[string]any
	readJSON(t, resp, &agents)
	if len(agents) == 0 {
		t.Fatal("no agents in test workspace")
	}
	runtimeID := agents[0]["runtime_id"].(string)

	resp = authRequest(t, "POST", "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":       fmt.Sprintf("Second Test Agent %d", time.Now().UnixNano()),
		"runtime_id": runtimeID,
		"visibility": "workspace",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateAgent: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var agent map[string]any
	readJSON(t, resp, &agent)
	id := agent["id"].(string)
	t.Cleanup(func() {
		authRequest(t, "POST", "/api/agents/"+id+"/archive?workspace_id="+testWorkspaceID, nil)
	})
	return id
}

// createTaskAssignedToAgent creates a todo task assigned to the given agent.
func createTaskAssignedToAgent(t *testing.T, title, agentID string) string {
	t.Helper()
	resp := authRequest(t, "PUT", fmt.Sprintf("/api/tasks/%s", createTask(t, title)), map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	var task map[string]any
	readJSON(t, resp, &task)
	return task["id"].(string)
}

// createTask creates a basic todo task and returns its ID.
func createTask(t *testing.T, title string) string {
	t.Helper()
	resp := authRequest(t, "POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("CreateTask: expected 201, got %d: %s", resp.StatusCode, body)
	}
	var task map[string]any
	readJSON(t, resp, &task)
	return task["id"].(string)
}

// postComment posts a comment as the test member.
func postComment(t *testing.T, taskID, content string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequest(t, "POST", "/api/tasks/"+taskID+"/comments", body)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postComment: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

// postCommentAsAgent posts a comment with the X-Agent-ID header.
func postCommentAsAgent(t *testing.T, taskID, content, agentID string, parentID *string) string {
	t.Helper()
	body := map[string]any{
		"content": content,
		"type":    "comment",
	}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	resp := authRequestWithAgent(t, "POST", "/api/tasks/"+taskID+"/comments", body, agentID)
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("postCommentAsAgent: expected 201, got %d: %s", resp.StatusCode, b)
	}
	var comment map[string]any
	readJSON(t, resp, &comment)
	return comment["id"].(string)
}

// strPtr returns a pointer to a string.
func strPtr(s string) *string { return &s }

// TestCommentTriggerOnComment tests on_comment trigger scenarios end-to-end.
// Verifies that the agent task queue is populated correctly based on:
// - top-level vs threaded comments
// - member vs agent thread starters
// - presence/absence of @mentions
func TestCommentTriggerOnComment(t *testing.T) {
	agentID := getAgentID(t)
	taskID := createTaskAssignedToAgent(t, "Comment trigger integration test", agentID)
	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	t.Run("top-level comment without mentions triggers agent", func(t *testing.T) {
		clearRuns(t, taskID)
		postComment(t, taskID, "Please fix this bug", nil)
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("top-level comment mentioning only others suppresses trigger", func(t *testing.T) {
		clearRuns(t, taskID)
		// Mention a fake agent UUID that is not the assignee.
		content := "[@SomeoneElse](mention://agent/00000000-0000-0000-0000-000000000001) what do you think?"
		postComment(t, taskID, content, nil)
		if n := countPendingRuns(t, taskID); n != 0 {
			t.Errorf("expected 0 pending tasks, got %d", n)
		}
	})

	t.Run("top-level comment mentioning assignee triggers agent", func(t *testing.T) {
		clearRuns(t, taskID)
		content := fmt.Sprintf("[@Agent](mention://agent/%s) fix this", agentID)
		postComment(t, taskID, content, nil)
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	t.Run("reply to agent thread without mentions triggers agent", func(t *testing.T) {
		clearRuns(t, taskID)
		// Agent starts a thread.
		threadID := postCommentAsAgent(t, taskID, "I analyzed the task.", agentID, nil)
		// Member replies in the agent's thread.
		postComment(t, taskID, "Looks good, please proceed", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task, got %d", n)
		}
	})

	// Regression guard for #1301: the assignee on_comment path must record
	// the NEW reply as trigger_comment_id, not the thread root. Otherwise
	// the daemon feeds stale content to the agent prompt, which with
	// `--resume` sessions surfaces as "already replied, no further action".
	// Reply placement (flat-thread grouping) is handled downstream in
	// TaskService.createAgentComment, not here.
	t.Run("reply records new comment id (not thread root) as trigger_comment_id", func(t *testing.T) {
		clearRuns(t, taskID)
		threadID := postCommentAsAgent(t, taskID, "First pass analysis.", agentID, nil)
		replyID := postComment(t, taskID, "Please also check the edge case", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Fatalf("expected 1 pending task, got %d", n)
		}
		if got := latestTriggerCommentID(t, taskID); got != replyID {
			t.Errorf("trigger_comment_id = %q, want reply id %q (thread root was %q)",
				got, replyID, threadID)
		}
	})

	t.Run("reply to member thread without mentions falls back to assignee", func(t *testing.T) {
		clearRuns(t, taskID)
		// Member starts a thread.
		threadID := postComment(t, taskID, "Hey team, what do you think?", nil)
		// Clear the task that was created by the top-level comment.
		clearRuns(t, taskID)
		// Another member reply (same user in this test, but the key is parent is by member).
		postComment(t, taskID, "I agree with you", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (assignee fallback), got %d", n)
		}
	})

	t.Run("reply to member thread after agent replied triggers agent", func(t *testing.T) {
		clearRuns(t, taskID)
		// Member starts a thread (top-level comment).
		threadID := postComment(t, taskID, "Please fix this bug", nil)
		clearRuns(t, taskID)
		// Agent replies in the thread.
		postCommentAsAgent(t, taskID, "Working on it, found the root cause.", agentID, strPtr(threadID))
		// Member follows up in the same thread without @mentioning the agent.
		postComment(t, taskID, "Great, please also check the edge case", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (agent participated in thread), got %d", n)
		}
	})

	t.Run("reply to member thread mentioning assignee triggers agent", func(t *testing.T) {
		clearRuns(t, taskID)
		// Member starts a thread.
		threadID := postComment(t, taskID, "Question about this", nil)
		clearRuns(t, taskID)
		// Reply mentioning the assignee agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help with this?", agentID)
		postComment(t, taskID, content, strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 0 {
			// The mention of the assignee agent unblocks on_comment but
			// the assignee-mention path in on_mention skips the assignee.
			// Either 0 or 1 is acceptable depending on the on_comment logic.
			// With our implementation: isReplyToMemberThread returns false
			// (assignee mentioned), and commentMentionsOthersButNotAssignee
			// returns false (assignee is mentioned). So on_comment triggers.
			// Let's re-check.
		}
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in member thread), got %d", n)
		}
	})

	t.Run("reply to member thread that @mentioned assignee triggers without re-mention", func(t *testing.T) {
		clearRuns(t, taskID)
		// Member starts a thread that @mentions the assignee agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, taskID, content, nil)
		// Clear the task created by the top-level mention.
		clearRuns(t, taskID)
		// Reply in the thread WITHOUT re-mentioning the assignee.
		postComment(t, taskID, "Here is more context for you", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (assignee mentioned in thread root), got %d", n)
		}
	})
}

// TestCommentTriggerAtAllSuppression verifies that @all mentions do not
// trigger agent execution — @all is a broadcast, not a direct request.
func TestCommentTriggerAtAllSuppression(t *testing.T) {
	agentID := getAgentID(t)
	taskID := createTaskAssignedToAgent(t, "@all suppression test", agentID)
	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	t.Run("top-level @all comment suppresses on_comment", func(t *testing.T) {
		clearRuns(t, taskID)
		postComment(t, taskID, "[@All](mention://all/all) heads up everyone", nil)
		if n := countPendingRuns(t, taskID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all should not trigger agent), got %d", n)
		}
	})

	t.Run("@all in agent thread suppresses on_comment", func(t *testing.T) {
		clearRuns(t, taskID)
		threadID := postCommentAsAgent(t, taskID, "Here is my analysis.", agentID, nil)
		postComment(t, taskID, "[@All](mention://all/all) FYI for the team", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 0 {
			t.Errorf("expected 0 pending tasks (@all in agent thread), got %d", n)
		}
	})

	// SILI-5411: @all suppresses only the IMPLICIT routes. An explicit @agent in
	// the same comment is a direct request and must still enqueue that agent.
	t.Run("@all with explicit @agent still triggers the agent", func(t *testing.T) {
		clearRuns(t, taskID)
		content := fmt.Sprintf("[@All](mention://all/all) heads up — [@Agent](mention://agent/%s) please take this", agentID)
		postComment(t, taskID, content, nil)
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (@all must not swallow the explicit @agent), got %d", n)
		}
	})
}

// TestCommentTriggerOnAssignNoStatusGate verifies that assigning an agent to
// a non-todo task still triggers the agent (status gate was removed).
func TestCommentTriggerOnAssignNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	// Create an in_progress task.
	taskID := createTask(t, "On-assign status gate test")
	resp := authRequest(t, "PUT", "/api/tasks/"+taskID, map[string]any{
		"status": "in_progress",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	// Assign the agent — should trigger despite non-todo status.
	resp = authRequest(t, "PUT", "/api/tasks/"+taskID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("assign agent: expected 200, got %d: %s", resp.StatusCode, body)
	}
	resp.Body.Close()

	if n := countPendingRuns(t, taskID); n != 1 {
		t.Errorf("expected 1 pending task after assigning to in_progress task, got %d", n)
	}
}

// TestCommentTriggerOnMentionNoStatusGate verifies that @mentioning an agent
// on a done task still triggers the agent (no status gate on on_mention).
func TestCommentTriggerOnMentionNoStatusGate(t *testing.T) {
	agentID := getAgentID(t)

	// Create a done task (not assigned to agent).
	taskID := createTask(t, "On-mention done task test")
	resp := authRequest(t, "PUT", "/api/tasks/"+taskID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	// @mention the agent on a done task — should still trigger.
	content := fmt.Sprintf("[@Agent](mention://agent/%s) found a problem here", agentID)
	postComment(t, taskID, content, nil)

	if n := countPendingRuns(t, taskID); n != 1 {
		t.Errorf("expected 1 pending task after @mention on done task, got %d", n)
	}
}

// TestCommentTriggerThreadExplicitMentions verifies that a member root @mention
// owns plain replies in that thread, while explicit mentions in the reply still
// trigger normally.
func TestCommentTriggerThreadExplicitMentions(t *testing.T) {
	agentID := getAgentID(t)

	// Create an task NOT assigned to the agent, so on_comment won't fire.
	taskID := createTask(t, "Thread-inherited mention test")
	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	t.Run("plain reply in thread routes to root mention owner", func(t *testing.T) {
		clearRuns(t, taskID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you review this?", agentID)
		threadID := postComment(t, taskID, content, nil)
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Fatalf("expected 1 pending task after initial mention, got %d", n)
		}
		// Clear the task so we can test the reply independently.
		clearRuns(t, taskID)
		// Reply in the thread WITHOUT mentioning the agent.
		postComment(t, taskID, "Here is more context for you", strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task from root mention owner, got %d", n)
		}
	})

	t.Run("plain reply to multi-agent root routes only first mention", func(t *testing.T) {
		clearRuns(t, taskID)
		agentB := createSecondAgent(t)
		content := fmt.Sprintf(
			"[@AgentA](mention://agent/%s) [@AgentB](mention://agent/%s) can you both review this?",
			agentID,
			agentB,
		)
		threadID := postComment(t, taskID, content, nil)
		if n := countPendingRunsForAgent(t, taskID, agentID); n != 1 {
			t.Fatalf("expected 1 pending root task for first agent, got %d", n)
		}
		if n := countPendingRunsForAgent(t, taskID, agentB); n != 1 {
			t.Fatalf("expected 1 pending root task for second agent, got %d", n)
		}
		clearRuns(t, taskID)

		postComment(t, taskID, "Here is more context for you both", strPtr(threadID))
		if n := countPendingRunsForAgent(t, taskID, agentID); n != 1 {
			t.Errorf("expected 1 pending reply task for first agent, got %d", n)
		}
		if n := countPendingRunsForAgent(t, taskID, agentB); n != 0 {
			t.Errorf("expected 0 pending reply tasks for second agent, got %d", n)
		}
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected exactly 1 pending task after multi-agent root reply, got %d", n)
		}
	})

	t.Run("reply does not double-trigger when re-mentioning same agent", func(t *testing.T) {
		clearRuns(t, taskID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) help", agentID)
		threadID := postComment(t, taskID, content, nil)
		clearRuns(t, taskID)
		// Reply also @mentions the same agent — should still be just 1 task.
		reply := fmt.Sprintf("[@Agent](mention://agent/%s) any update?", agentID)
		postComment(t, taskID, reply, strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (no duplicate), got %d", n)
		}
	})

	t.Run("reply mentioning only a member does not inherit agent mention", func(t *testing.T) {
		clearRuns(t, taskID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) can you help?", agentID)
		threadID := postComment(t, taskID, content, nil)
		clearRuns(t, taskID)
		// Reply mentions only a member — should NOT inherit parent's agent mention.
		reply := fmt.Sprintf("cc [@Someone](mention://member/%s)", testUserID)
		postComment(t, taskID, reply, strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 0 {
			t.Errorf("expected 0 pending tasks (member-only reply should not inherit agent mention), got %d", n)
		}
	})

	t.Run("reply mentioning a different agent does not inherit parent agent", func(t *testing.T) {
		clearRuns(t, taskID)
		agentB := createSecondAgent(t)
		// Top-level comment @mentions agent A.
		content := fmt.Sprintf("[@AgentA](mention://agent/%s) please review", agentID)
		threadID := postComment(t, taskID, content, nil)
		clearRuns(t, taskID)
		// Reply @mentions agent B — should trigger ONLY agent B, not agent A.
		reply := fmt.Sprintf("[@AgentB](mention://agent/%s) can you also look?", agentB)
		postComment(t, taskID, reply, strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (only agent B), got %d", n)
		}
	})

	t.Run("reply mentioning same agent and member triggers via explicit mention", func(t *testing.T) {
		clearRuns(t, taskID)
		// Top-level comment @mentions the agent.
		content := fmt.Sprintf("[@Agent](mention://agent/%s) review this", agentID)
		threadID := postComment(t, taskID, content, nil)
		clearRuns(t, taskID)
		// Reply re-mentions the same agent along with a member — triggers via the reply's own mention.
		reply := fmt.Sprintf("[@Agent](mention://agent/%s) and cc [@Someone](mention://member/%s)", agentID, testUserID)
		postComment(t, taskID, reply, strPtr(threadID))
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Errorf("expected 1 pending task (reply mentions agent explicitly), got %d", n)
		}
	})
}

// TestDeleteCommentCancelsTriggeredTasks verifies that deleting a comment
// also cancels any active tasks that were triggered by it. Without this,
// the daemon would still claim the queued task after the FK SET NULL
// nullified its trigger_comment_id, and the agent would either run with a
// stale prompt (race during claim) or with a generic "you are assigned"
// prompt that has no record of the now-deleted user request — both of
// which manifest as "the agent still sees the deleted comment".
func TestDeleteCommentCancelsTriggeredRuns(t *testing.T) {
	agentID := getAgentID(t)
	taskID := createTaskAssignedToAgent(t, "Delete-comment cancels task test", agentID)
	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	t.Run("deleting trigger comment cancels its queued task", func(t *testing.T) {
		clearRuns(t, taskID)
		commentID := postComment(t, taskID, "Please fix this bug", nil)
		if n := countPendingRuns(t, taskID); n != 1 {
			t.Fatalf("expected 1 pending task before delete, got %d", n)
		}

		resp := authRequest(t, "DELETE", "/api/comments/"+commentID, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DeleteComment: expected 204, got %d", resp.StatusCode)
		}

		if n := countPendingRuns(t, taskID); n != 0 {
			t.Errorf("expected 0 pending tasks after deleting trigger comment, got %d", n)
		}
	})
}

// TestCommentTriggerCoalescing verifies that rapid-fire comments don't create
// duplicate tasks (coalescing dedup).
func TestCommentTriggerCoalescing(t *testing.T) {
	agentID := getAgentID(t)
	taskID := createTaskAssignedToAgent(t, "Coalescing test", agentID)
	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	// Post two comments rapidly — only 1 task should be created (coalescing).
	postComment(t, taskID, "First comment", nil)
	postComment(t, taskID, "Second comment", nil)

	if n := countPendingRuns(t, taskID); n != 1 {
		t.Errorf("expected 1 pending task (coalescing), got %d", n)
	}
}

// TestCommentTriggerMentionAssigneeDoneTask verifies that @mentioning the
// assigned agent on a done task still triggers execution. Previously the
// assignee was unconditionally skipped in the mention path (assuming
// on_comment handled it), but on_comment is suppressed for terminal statuses.
func TestCommentTriggerMentionAssigneeDoneTask(t *testing.T) {
	agentID := getAgentID(t)

	// Create an task assigned to the agent, then mark it done.
	taskID := createTaskAssignedToAgent(t, "Mention-assignee-done test", agentID)
	clearRuns(t, taskID) // clear any tasks from assignment
	resp := authRequest(t, "PUT", "/api/tasks/"+taskID, map[string]any{
		"status": "done",
	})
	resp.Body.Close()

	t.Cleanup(func() {
		clearRuns(t, taskID)
		resp := authRequest(t, "DELETE", "/api/tasks/"+taskID, nil)
		resp.Body.Close()
	})

	// @mention the assigned agent on the done task — should trigger.
	content := fmt.Sprintf("[@Agent](mention://agent/%s) reopen this please", agentID)
	postComment(t, taskID, content, nil)

	if n := countPendingRuns(t, taskID); n != 1 {
		t.Errorf("expected 1 pending task after @mention of assignee on done task, got %d", n)
	}
}
