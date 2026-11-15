package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/silieco-ai/silieco/core/internal/util"
)

func createCommentTriggerPreviewTask(t *testing.T, title string, assigneeType, assigneeID string) string {
	t.Helper()
	ctx := context.Background()

	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET task_counter = GREATEST(task_counter, (SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING task_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next task number: %v", err)
	}

	var assigneeTypeArg any
	var assigneeIDArg any
	if assigneeType != "" {
		assigneeTypeArg = assigneeType
	}
	if assigneeID != "" {
		assigneeIDArg = assigneeID
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'member', $2, $3, $4, $5, $6)
		RETURNING id
	`, testWorkspaceID, testUserID, title, assigneeTypeArg, assigneeIDArg, number).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	return taskID
}

func previewCommentTriggersForTest(t *testing.T, taskID string, body any) CommentTriggerPreviewResponse {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/tasks/"+taskID+"/comments/trigger-preview", body)
	r = withURLParam(r, "id", taskID)
	testHandler.PreviewCommentTriggers(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewCommentTriggers: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	return resp
}

func postCommentForTriggerPreviewTest(t *testing.T, taskID string, body map[string]any) string {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/tasks/"+taskID+"/comments", body)
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	return resp.ID
}

func insertMemberRootCommentForTriggerPreviewTest(t *testing.T, taskID, content string) string {
	t.Helper()

	var commentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, taskID, testUserID, content).Scan(&commentID); err != nil {
		t.Fatalf("insert member root comment: %v", err)
	}
	return commentID
}

func updateCommentForTriggerPreviewTest(t *testing.T, commentID string, body map[string]any) {
	t.Helper()

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPut, "/api/comments/"+commentID, body)
	r = withURLParam(r, "commentId", commentID)
	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func countQueuedCommentTriggerRuns(t *testing.T, taskID, agentID string) int {
	t.Helper()

	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued tasks: %v", err)
	}
	return n
}

func countCommentTriggerRunsWithStatus(t *testing.T, taskID, agentID, status string) int {
	t.Helper()

	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = $3
	`, taskID, agentID, status).Scan(&n); err != nil {
		t.Fatalf("count %s tasks: %v", status, err)
	}
	return n
}

func createCommentTriggerPreviewSquad(t *testing.T, name, leaderID string) string {
	t.Helper()

	var squadID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, name, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})
	return squadID
}

func requirePreviewAgents(t *testing.T, preview CommentTriggerPreviewResponse, wantIDs ...string) {
	t.Helper()
	if len(preview.Agents) != len(wantIDs) {
		t.Fatalf("preview agents = %+v, want ids %v", preview.Agents, wantIDs)
	}
	got := make(map[string]struct{}, len(preview.Agents))
	for _, agent := range preview.Agents {
		got[agent.ID] = struct{}{}
	}
	for _, want := range wantIDs {
		if _, ok := got[want]; !ok {
			t.Fatalf("preview agents = %+v, missing id %s", preview.Agents, want)
		}
	}
}

func TestPreviewCommentTriggers_PlainReplyToMemberRootMentionRoutesToMentionedAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	waltID := createHandlerTestAgent(t, "Preview Member Reply Walt", nil)
	kimID := createHandlerTestAgent(t, "Preview Member Reply Kim", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger preview member reply fallback", "agent", waltID)

	topLevelPreview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content: "hello from the root composer",
	})
	requirePreviewAgents(t, topLevelPreview, waltID)

	rootContent := fmt.Sprintf("[@Kim](mention://agent/%s) can you inspect this?", kimID)
	rootID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": rootContent})
	if got := countQueuedCommentTriggerRuns(t, taskID, kimID); got != 1 {
		t.Fatalf("root mention queued Kim tasks before completion = %d, want 1", got)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_run SET status = 'completed'
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, kimID); err != nil {
		t.Fatalf("complete Kim root run: %v", err)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, waltID); got != 0 {
		t.Fatalf("fixture queued Walt tasks = %d, want 0", got)
	}

	replyContent := "plain reply with no mention"
	replyParentID := rootID
	replyBody := map[string]any{
		"content":   replyContent,
		"parent_id": rootID,
	}
	replyPreview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content:  replyContent,
		ParentID: &replyParentID,
	})
	requirePreviewAgents(t, replyPreview, kimID)
	if replyPreview.Agents[0].Source != string(commentTriggerSourceConversation) {
		t.Fatalf("reply preview source = %q, want %q", replyPreview.Agents[0].Source, commentTriggerSourceConversation)
	}

	postCommentForTriggerPreviewTest(t, taskID, replyBody)
	if got := countQueuedCommentTriggerRuns(t, taskID, kimID); got != 1 {
		t.Fatalf("plain reply queued Kim tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, waltID); got != 0 {
		t.Fatalf("plain reply queued Walt tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_PlainReplyToMultiAgentRootRoutesFirstMentionedOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Preview Multi Root Assignee", nil)
	agentAID := createHandlerTestAgent(t, "Preview Multi Root A", nil)
	agentBID := createHandlerTestAgent(t, "Preview Multi Root B", nil)
	taskID := createCommentTriggerPreviewTask(t, "multi-agent root owner reply", "agent", assigneeID)

	rootContent := fmt.Sprintf(
		"[@A](mention://agent/%s) [@B](mention://agent/%s) can you both inspect this?",
		agentAID,
		agentBID,
	)
	rootID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": rootContent})
	if got := countQueuedCommentTriggerRuns(t, taskID, agentAID); got != 1 {
		t.Fatalf("root mention queued agent A tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, agentBID); got != 1 {
		t.Fatalf("root mention queued agent B tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("root mention queued assignee tasks = %d, want 0", got)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_run SET status = 'completed'
		WHERE task_id = $1 AND agent_id IN ($2, $3) AND status = 'queued'
	`, taskID, agentAID, agentBID); err != nil {
		t.Fatalf("complete root owner tasks: %v", err)
	}

	replyContent := "plain reply with no mention"
	replyParentID := rootID
	replyPreview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content:  replyContent,
		ParentID: &replyParentID,
	})
	requirePreviewAgents(t, replyPreview, agentAID)
	if replyPreview.Agents[0].Source != string(commentTriggerSourceConversation) {
		t.Fatalf("reply preview source = %q, want %q", replyPreview.Agents[0].Source, commentTriggerSourceConversation)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   replyContent,
		"parent_id": rootID,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, agentAID); got != 1 {
		t.Fatalf("plain reply queued agent A tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, agentBID); got != 0 {
		t.Fatalf("plain reply queued agent B tasks = %d, want 0", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("plain reply queued assignee tasks = %d, want 0", got)
	}
}

// TestPreviewCommentTriggers_SquadAssigneePlainReplyKeepsRootMentionOwner is the
// cascade replacement for the old SILI-3744 inherited-mention scenario:
//
//   - Task is assigned to a SQUAD (leader L).
//   - Member root comment @mentions another agent (Kim).
//   - Member posts a plain reply with no mention of its own ("hello").
//
// New cascade behavior: the root's explicit @agent establishes the thread
// owner, so the plain reply returns to Kim instead of the squad assignee.
func TestPreviewCommentTriggers_SquadAssigneePlainReplyKeepsRootMentionOwner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Preview Squad Leader L", nil)
	kimID := createHandlerTestAgent(t, "Preview Squad Mention Kim", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Preview Squad Reply Routing", leaderID)
	taskID := createCommentTriggerPreviewTask(t, "squad reply mention inheritance SILI-3744", "squad", squadID)

	// Sanity: a plain top-level "hello" by a member on this squad-assigned
	// task wakes the leader (no @mention is routing the work).
	topLevelPreview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content: "hello",
	})
	requirePreviewAgents(t, topLevelPreview, leaderID)

	rootContent := fmt.Sprintf("[@Kim](mention://agent/%s) can you take a look?", kimID)
	rootID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": rootContent})
	if got := countQueuedCommentTriggerRuns(t, taskID, leaderID); got != 0 {
		t.Fatalf("fixture queued leader tasks = %d, want 0", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, kimID); got != 1 {
		t.Fatalf("root mention queued Kim tasks = %d, want 1", got)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_run SET status = 'completed'
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, kimID); err != nil {
		t.Fatalf("complete Kim root run: %v", err)
	}

	// Composer preview of a plain reply ("hello") to that root.
	// Expected: only Kim fires via the root conversation owner.
	replyContent := "hello"
	replyParentID := rootID
	replyBody := map[string]any{
		"content":   replyContent,
		"parent_id": rootID,
	}
	replyPreview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content:  replyContent,
		ParentID: &replyParentID,
	})
	requirePreviewAgents(t, replyPreview, kimID)
	if replyPreview.Agents[0].Source != string(commentTriggerSourceConversation) {
		t.Fatalf("reply preview source = %q, want %q (conversation owner), got %+v",
			replyPreview.Agents[0].Source, commentTriggerSourceConversation, replyPreview.Agents)
	}

	// Verify the create path matches the preview.
	postCommentForTriggerPreviewTest(t, taskID, replyBody)
	if got := countQueuedCommentTriggerRuns(t, taskID, leaderID); got != 0 {
		t.Fatalf("after plain reply on squad task: expected 0 leader tasks, got %d", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, kimID); got != 1 {
		t.Fatalf("after plain reply on squad task: expected 1 Kim task, got %d", got)
	}
}

func TestPreviewCommentTriggers_ReturnsMentionedAgentsAndSuppressFiltersCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentA := createHandlerTestAgent(t, "Preview Mention A", nil)
	agentB := createHandlerTestAgent(t, "Preview Mention B", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger preview mentions", "", "")
	content := fmt.Sprintf("[@A](mention://agent/%s) [@B](mention://agent/%s) please inspect", agentA, agentB)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 2 {
		t.Fatalf("expected 2 preview agents, got %d: %+v", got, preview.Agents)
	}
	for _, agent := range preview.Agents {
		if agent.Source != string(commentTriggerSourceMentionAgent) {
			t.Fatalf("preview source = %q, want %q", agent.Source, commentTriggerSourceMentionAgent)
		}
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{agentB},
	})

	if got := countQueuedCommentTriggerRuns(t, taskID, agentA); got != 1 {
		t.Fatalf("unsuppressed mentioned agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, agentB); got != 0 {
		t.Fatalf("suppressed mentioned agent queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_ExplicitMentionSuppressesAssigneeFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Exclusive Mention Assignee", nil)
	mentionedID := createHandlerTestAgent(t, "Exclusive Mention Target", nil)
	taskID := createCommentTriggerPreviewTask(t, "explicit mention is exclusive", "agent", assigneeID)
	content := fmt.Sprintf("[@Target](mention://agent/%s) please take this", mentionedID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, mentionedID)
	if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, mentionedID); got != 1 {
		t.Fatalf("mentioned agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("assignee fallback queued tasks = %d, want 0 for explicit mention", got)
	}
}

func TestCreateComment_ExplicitMentionKeepsPendingRouteWithoutDuplicateRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	mentionedID := createHandlerTestAgent(t, "Explicit Pending Route Target", nil)
	taskID := createCommentTriggerPreviewTask(t, "explicit mention pending route", "", "")
	content := fmt.Sprintf("[@Target](mention://agent/%s) please take this", mentionedID)

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, mentionedID); got != 1 {
		t.Fatalf("initial mention queued tasks = %d, want 1", got)
	}

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content + " again"})
	requirePreviewAgents(t, preview, mentionedID)
	if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content + " again"})
	if got := countQueuedCommentTriggerRuns(t, taskID, mentionedID); got != 1 {
		t.Fatalf("duplicate pending mention queued tasks = %d, want 1", got)
	}
}

func TestCreateComment_TopLevelNewThreadFallsBackToAssignee(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Conversation Assignee", nil)
	conversationAgentID := createHandlerTestAgent(t, "Conversation Target", nil)
	taskID := createCommentTriggerPreviewTask(t, "top-level new thread falls back to assignee", "agent", assigneeID)

	rootID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": fmt.Sprintf("[@Target](mention://agent/%s) ping test", conversationAgentID),
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, conversationAgentID); got != 1 {
		t.Fatalf("initial mention queued conversation agent tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("initial mention queued assignee tasks = %d, want 0", got)
	}

	var conversationRunID string
	if err := testPool.QueryRow(ctx, `
		UPDATE agent_run
		SET status = 'completed', completed_at = now()
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
		RETURNING id
	`, taskID, conversationAgentID).Scan(&conversationRunID); err != nil {
		t.Fatalf("complete initial conversation run: %v", err)
	}

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/tasks/"+taskID+"/comments", map[string]any{
		"content":   "Pong",
		"parent_id": rootID,
	})
	r = withURLParam(r, "id", taskID)
	r.Header.Set("X-Agent-ID", conversationAgentID)
	r.Header.Set("X-Run-ID", conversationRunID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent reply: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	followUpContent := "人生的意义是什么?"
	preview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content: followUpContent,
	})
	requirePreviewAgents(t, preview, assigneeID)
	if preview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
		t.Fatalf("new thread preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceTaskAssignee)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": followUpContent,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, conversationAgentID); got != 0 {
		t.Fatalf("new thread queued conversation agent tasks = %d, want 0", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 1 {
		t.Fatalf("new thread queued assignee tasks = %d, want 1", got)
	}
}

func TestPreviewCommentTriggers_MemberMentionSuppressesAssigneeFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Member Mention Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "member mention suppresses fallback", "agent", assigneeID)
	content := fmt.Sprintf("[@Human](mention://member/%s) can you answer?", testUserID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	requirePreviewAgents(t, preview)

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("assignee fallback queued tasks = %d, want 0 for member mention", got)
	}
}

func TestCreateComment_ThreadParentQueuesParentAndPromotesDeferredFallback(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Thread Parent Assignee", nil)
	parentAgentID := createHandlerTestAgent(t, "Thread Parent Owner", nil)
	taskID := createCommentTriggerPreviewTask(t, "thread parent deferred fallback", "agent", assigneeID)

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'I am handling this')
		RETURNING id
	`, testWorkspaceID, taskID, parentAgentID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	replyContent := "can you follow up here?"
	preview := previewCommentTriggersForTest(t, taskID, CommentTriggerPreviewRequest{
		Content:  replyContent,
		ParentID: &parentCommentID,
	})
	requirePreviewAgents(t, preview, parentAgentID)
	if preview.Agents[0].Source != string(commentTriggerSourceThreadParent) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceThreadParent)
	}

	replyID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   replyContent,
		"parent_id": parentCommentID,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, parentAgentID); got != 1 {
		t.Fatalf("parent agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("assignee queued tasks before timeout = %d, want 0", got)
	}

	var primaryRunID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, parentAgentID).Scan(&primaryRunID); err != nil {
		t.Fatalf("load primary run: %v", err)
	}

	var fallbackRunID, escalationForRunID, triggerCommentID string
	var fireAt time.Time
	if err := testPool.QueryRow(ctx, `
		SELECT id, escalation_for_run_id, trigger_comment_id, fire_at
		FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'deferred'
	`, taskID, assigneeID).Scan(&fallbackRunID, &escalationForRunID, &triggerCommentID, &fireAt); err != nil {
		t.Fatalf("load deferred fallback run: %v", err)
	}
	if escalationForRunID != primaryRunID {
		t.Fatalf("deferred fallback escalation_for_run_id = %s, want %s", escalationForRunID, primaryRunID)
	}
	if triggerCommentID != replyID {
		t.Fatalf("deferred fallback trigger_comment_id = %s, want %s", triggerCommentID, replyID)
	}
	if fireAt.Before(time.Now().Add(4*time.Minute)) || fireAt.After(time.Now().Add(6*time.Minute)) {
		t.Fatalf("deferred fallback fire_at = %s, want about 5 minutes from now", fireAt.Format(time.RFC3339))
	}

	if _, err := testPool.Exec(ctx, `UPDATE agent_run SET fire_at = now() - interval '1 second' WHERE id = $1`, fallbackRunID); err != nil {
		t.Fatalf("make deferred fallback due: %v", err)
	}
	if err := testHandler.AgentRunService.PromoteDueDeferredRunsForRuntime(ctx, util.MustParseUUID(testRuntimeID)); err != nil {
		t.Fatalf("promote due deferred fallback: %v", err)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 1 {
		t.Fatalf("assignee queued tasks after timeout promotion = %d, want 1", got)
	}
}

func TestCreateComment_ThreadParentSkipsDeferredFallbackWhenParentIsAssignee(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Thread Parent Same Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "thread parent same as assignee", "agent", assigneeID)

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'I am handling this')
		RETURNING id
	`, testWorkspaceID, taskID, assigneeID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   "can you follow up here?",
		"parent_id": parentCommentID,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 1 {
		t.Fatalf("parent assignee queued tasks = %d, want 1", got)
	}
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "deferred"); got != 0 {
		t.Fatalf("deferred fallback for same parent assignee = %d, want 0", got)
	}
}

func TestCreateComment_ThreadParentEscalationCanBeDisabled(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var previousSettings string
	if err := testPool.QueryRow(ctx,
		`SELECT COALESCE(settings, '{}'::jsonb)::text FROM workspace WHERE id = $1`,
		testWorkspaceID,
	).Scan(&previousSettings); err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE workspace SET settings = '{"comment_routing":{"escalation_seconds":0}}'::jsonb WHERE id = $1`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("disable comment routing escalation: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1::jsonb WHERE id = $2`, previousSettings, testWorkspaceID)
	})

	assigneeID := createHandlerTestAgent(t, "Thread Parent Disabled Assignee", nil)
	parentAgentID := createHandlerTestAgent(t, "Thread Parent Disabled Owner", nil)
	taskID := createCommentTriggerPreviewTask(t, "thread parent disabled fallback", "agent", assigneeID)

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'I am handling this')
		RETURNING id
	`, testWorkspaceID, taskID, parentAgentID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   "can you follow up here?",
		"parent_id": parentCommentID,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, parentAgentID); got != 1 {
		t.Fatalf("parent agent queued tasks = %d, want 1", got)
	}
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "deferred"); got != 0 {
		t.Fatalf("disabled deferred assignee fallback = %d, want 0", got)
	}
}

func TestCreateComment_ParentAgentReplyCancelsPromotedFallbackBeforeClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Thread Parent Cancel Assignee", nil)
	parentAgentID := createHandlerTestAgent(t, "Thread Parent Cancel Owner", nil)
	taskID := createCommentTriggerPreviewTask(t, "thread parent deferred cancel", "agent", assigneeID)

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'I am handling this')
		RETURNING id
	`, testWorkspaceID, taskID, parentAgentID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	memberReplyID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   "can you follow up here?",
		"parent_id": parentCommentID,
	})
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "deferred"); got != 1 {
		t.Fatalf("deferred assignee fallback before parent ack = %d, want 1", got)
	}

	var primaryRunID, fallbackRunID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_run
		WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'
	`, taskID, parentAgentID).Scan(&primaryRunID); err != nil {
		t.Fatalf("load primary run: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		UPDATE agent_run
		SET fire_at = now() - interval '1 second'
		WHERE task_id = $1 AND agent_id = $2 AND status = 'deferred'
		RETURNING id
	`, taskID, assigneeID).Scan(&fallbackRunID); err != nil {
		t.Fatalf("make deferred fallback due: %v", err)
	}
	if err := testHandler.AgentRunService.PromoteDueDeferredRunsForRuntime(ctx, util.MustParseUUID(testRuntimeID)); err != nil {
		t.Fatalf("promote due deferred fallback: %v", err)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 1 {
		t.Fatalf("queued assignee fallback before parent ack = %d, want 1", got)
	}

	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/tasks/"+taskID+"/comments", map[string]any{
		"content":   "acknowledged",
		"parent_id": memberReplyID,
	})
	r = withURLParam(r, "id", taskID)
	r.Header.Set("X-Agent-ID", parentAgentID)
	r.Header.Set("X-Run-ID", primaryRunID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent ack comment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "deferred"); got != 0 {
		t.Fatalf("deferred assignee fallback after parent ack = %d, want 0", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("queued assignee fallback after parent ack = %d, want 0", got)
	}
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "cancelled"); got != 1 {
		t.Fatalf("cancelled assignee fallback after parent ack = %d, want 1", got)
	}
	claimed, err := testHandler.AgentRunService.ClaimRun(ctx, util.MustParseUUID(assigneeID))
	if err != nil {
		t.Fatalf("claim assignee fallback after parent ack: %v", err)
	}
	if claimed != nil {
		t.Fatalf("assignee claimed cancelled fallback %s after parent ack; fallback task was %s", util.UUIDToString(claimed.ID), fallbackRunID)
	}
}

func TestStartRunCancelsPromotedFallbackBeforeAssigneeCanClaim(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	assigneeID := createHandlerTestAgent(t, "Thread Parent Start Cancel Assignee", nil)
	parentAgentID := createHandlerTestAgent(t, "Thread Parent Start Cancel Owner", nil)
	taskID := createCommentTriggerPreviewTask(t, "thread parent start cancels fallback", "agent", assigneeID)

	var parentCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, task_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'I am handling this')
		RETURNING id
	`, testWorkspaceID, taskID, parentAgentID).Scan(&parentCommentID); err != nil {
		t.Fatalf("insert parent agent comment: %v", err)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":   "can you follow up here?",
		"parent_id": parentCommentID,
	})
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "deferred"); got != 1 {
		t.Fatalf("deferred assignee fallback before start = %d, want 1", got)
	}

	primary, err := testHandler.AgentRunService.ClaimRun(ctx, util.MustParseUUID(parentAgentID))
	if err != nil {
		t.Fatalf("claim parent run: %v", err)
	}
	if primary == nil {
		t.Fatal("claim parent task returned nil")
	}

	var fallbackRunID string
	if err := testPool.QueryRow(ctx, `
		UPDATE agent_run
		SET fire_at = now() - interval '1 second'
		WHERE task_id = $1 AND agent_id = $2 AND status = 'deferred'
		RETURNING id
	`, taskID, assigneeID).Scan(&fallbackRunID); err != nil {
		t.Fatalf("make deferred fallback due: %v", err)
	}
	if err := testHandler.AgentRunService.PromoteDueDeferredRunsForRuntime(ctx, util.MustParseUUID(testRuntimeID)); err != nil {
		t.Fatalf("promote due deferred fallback: %v", err)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 1 {
		t.Fatalf("queued assignee fallback before parent start = %d, want 1", got)
	}

	if _, err := testHandler.AgentRunService.StartRun(ctx, primary.ID); err != nil {
		t.Fatalf("start parent run: %v", err)
	}

	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("queued assignee fallback after parent start = %d, want 0", got)
	}
	if got := countCommentTriggerRunsWithStatus(t, taskID, assigneeID, "cancelled"); got != 1 {
		t.Fatalf("cancelled assignee fallback after parent start = %d, want 1", got)
	}
	claimed, err := testHandler.AgentRunService.ClaimRun(ctx, util.MustParseUUID(assigneeID))
	if err != nil {
		t.Fatalf("claim assignee fallback after parent start: %v", err)
	}
	if claimed != nil {
		t.Fatalf("assignee claimed cancelled fallback %s after parent start; fallback task was %s", util.UUIDToString(claimed.ID), fallbackRunID)
	}
}

func TestPreviewCommentTriggers_EditExcludesSameCommentPendingRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Edit Preview Exclude Agent", nil)

	t.Run("agent assignee on-comment", func(t *testing.T) {
		taskID := createCommentTriggerPreviewTask(t, "edit preview assignee", "agent", agentID)
		commentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
			"content": "please start here",
		})
		if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, taskID, map[string]any{
			"content":            "actually start over here",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceTaskAssignee)
		}
	})

	t.Run("squad assignee on-comment", func(t *testing.T) {
		squadID := createCommentTriggerPreviewSquad(t, "Edit Preview Assignee Squad", agentID)
		taskID := createCommentTriggerPreviewTask(t, "edit preview squad assignee", "squad", squadID)
		commentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
			"content": "please coordinate this",
		})
		if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, taskID, map[string]any{
			"content":            "actually coordinate this instead",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceTaskAssignee)
		}
	})

	t.Run("direct agent mention", func(t *testing.T) {
		taskID := createCommentTriggerPreviewTask(t, "edit preview agent mention", "", "")
		content := fmt.Sprintf("[@Agent](mention://agent/%s) inspect this", agentID)
		commentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
			"content": content,
		})
		if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, taskID, map[string]any{
			"content":            content + " again",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
		}
	})

	t.Run("squad mention leader", func(t *testing.T) {
		squadID := createCommentTriggerPreviewSquad(t, "Edit Preview Mention Squad", agentID)
		taskID := createCommentTriggerPreviewTask(t, "edit preview squad mention", "", "")
		content := fmt.Sprintf("[@Squad](mention://squad/%s) inspect this", squadID)
		commentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
			"content": content,
		})
		if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
			t.Fatalf("queued tasks before edit preview = %d, want 1", got)
		}

		preview := previewCommentTriggersForTest(t, taskID, map[string]any{
			"content":            content + " again",
			"editing_comment_id": commentID,
		})
		requirePreviewAgents(t, preview, agentID)
		if preview.Agents[0].Source != string(commentTriggerSourceMentionSquadLeader) {
			t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionSquadLeader)
		}
	})
}

func TestPreviewCommentTriggers_EditExclusionDoesNotIgnoreOtherCommentPendingRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Edit Preview Other Pending Agent", nil)
	taskID := createCommentTriggerPreviewTask(t, "edit preview other pending", "", "")
	content := fmt.Sprintf("[@Agent](mention://agent/%s) inspect this", agentID)
	_ = postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": content,
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
		t.Fatalf("queued tasks before edit preview = %d, want 1", got)
	}
	editingCommentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": "plain follow-up with no mention",
	})

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{
		"content":            content,
		"editing_comment_id": editingCommentID,
	})
	requirePreviewAgents(t, preview, agentID)
	if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
	}
}

func TestUpdateComment_SuppressAgentIDsFiltersEditRetrigger(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentA := createHandlerTestAgent(t, "Edit Suppress A", nil)
	agentB := createHandlerTestAgent(t, "Edit Suppress B", nil)
	taskID := createCommentTriggerPreviewTask(t, "edit suppress agent ids", "", "")
	commentID := postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": "plain comment",
	})
	content := fmt.Sprintf("[@A](mention://agent/%s) [@B](mention://agent/%s) inspect this", agentA, agentB)

	updateCommentForTriggerPreviewTest(t, commentID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{agentB},
	})

	if got := countQueuedCommentTriggerRuns(t, taskID, agentA); got != 1 {
		t.Fatalf("unsuppressed agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, agentB); got != 0 {
		t.Fatalf("suppressed agent queued tasks = %d, want 0", got)
	}
}

func TestCreateComment_SuppressUnknownAgentIDIsNoop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Suppress Noop Agent", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger suppress noop", "", "")
	content := fmt.Sprintf("[@Agent](mention://agent/%s) please inspect", agentID)

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": content,
		"suppress_agent_ids": []string{
			"00000000-0000-0000-0000-000000000001",
		},
	})

	if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
		t.Fatalf("mentioned agent queued tasks = %d, want 1", got)
	}
}

func TestPreviewCommentTriggers_NoteReturnsNoAgents(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Preview Note Agent", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger note", "agent", agentID)
	content := fmt.Sprintf("/note [@Agent](mention://agent/%s) human-only context", agentID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 0 {
		t.Fatalf("note preview agents = %d, want 0: %+v", got, preview.Agents)
	}
}

func TestCreateComment_NoteMentionDoesNotQueueAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Create Note Agent", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger create note", "agent", agentID)
	content := fmt.Sprintf("/note [@Agent](mention://agent/%s) human-only context", agentID)

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})

	if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 0 {
		t.Fatalf("note create queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_AssigneeAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "Preview Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger assignee", "agent", agentID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": "can you continue here?"})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 assignee preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != agentID {
		t.Fatalf("preview agent id = %s, want %s", preview.Agents[0].ID, agentID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceTaskAssignee)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":            "can you continue here?",
		"suppress_agent_ids": []string{agentID},
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 0 {
		t.Fatalf("suppressed assignee queued tasks = %d, want 0", got)
	}
}

// TestPreviewCommentTriggers_AllPlusExplicitAgentMentionStillTriggers pins
// SILI-5411: `@all` only suppresses the IMPLICIT assignee auto-trigger. A comment
// that carries `@all` AND an explicit `@agent` must still enqueue that agent —
// the old ordering short-circuited on `@all` and dropped every trigger, so a
// "[@all] ... [@Preflight]" comment silently ran nothing.
func TestPreviewCommentTriggers_AllPlusExplicitAgentMentionStillTriggers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Preview All Assignee", nil)
	mentionedID := createHandlerTestAgent(t, "Preview All Mentioned", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger all plus mention", "agent", assigneeID)
	content := fmt.Sprintf("[@all](mention://all/all) heads up — [@Mentioned](mention://agent/%s) please take this", mentionedID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	// Only the explicitly named agent runs: @all does not fan out to agents and
	// does not resurrect the assignee fallback.
	requirePreviewAgents(t, preview, mentionedID)
	if preview.Agents[0].Source != string(commentTriggerSourceMentionAgent) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionAgent)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, mentionedID); got != 1 {
		t.Fatalf("mentioned agent queued tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("assignee queued tasks = %d, want 0 (@all suppresses the assignee fallback)", got)
	}
}

// TestPreviewCommentTriggers_AllPlusExplicitSquadMentionStillTriggers is the
// squad half of SILI-5411: an `@all` broadcast must not swallow an explicit
// `@squad` mention either — the squad leader still wakes.
func TestPreviewCommentTriggers_AllPlusExplicitSquadMentionStillTriggers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Preview All Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Preview All Squad "+t.Name(), leaderID)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger all plus squad mention", "", "")
	content := fmt.Sprintf("[@all](mention://all/all) FYI — [@Squad](mention://squad/%s) please pick this up", squadID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, leaderID)
	if preview.Agents[0].Source != string(commentTriggerSourceMentionSquadLeader) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionSquadLeader)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, leaderID); got != 1 {
		t.Fatalf("squad leader queued tasks = %d, want 1", got)
	}
}

// TestPreviewCommentTriggers_AllPlusMemberMentionStaysSuppressed guards the
// other side of the SILI-5411 reorder: `@all` alongside a `@member` mention (no
// agent/squad named) still triggers nothing.
func TestPreviewCommentTriggers_AllPlusMemberMentionStaysSuppressed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Preview All Member Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger all plus member mention", "agent", assigneeID)
	content := fmt.Sprintf("[@all](mention://all/all) and [@Member](mention://member/%s) heads up", testUserID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 0 {
		t.Fatalf("@all + @member preview agents = %d, want 0: %+v", got, preview.Agents)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": content})
	if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
		t.Fatalf("assignee queued tasks = %d, want 0", got)
	}
}

// TestPreviewCommentTriggers_MalformedMentionIDDoesNotPanic pins the review
// finding on PR #6048: MentionRe accepts any `[0-9a-fA-F-]+` id, so
// `mention://agent/-` parses as a real mention. The resolver used to hand that
// straight to the panicking parseUUID (util.MustParseUUID), turning attacker-
// controlled comment text into a 500 — and on the create path the comment row
// was already committed before the panic. Malformed ids must be reported as
// blocked mentions, exactly like a well-formed id that owns no entity.
func TestPreviewCommentTriggers_MalformedMentionIDDoesNotPanic(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Preview Malformed Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger malformed mention id", "agent", assigneeID)

	cases := []struct {
		name       string
		content    string
		targetType string
		targetID   string
		reason     DispatchReasonCode
	}{
		{
			name:       "bare dash agent id",
			content:    "[@Broken](mention://agent/-) please look",
			targetType: "agent",
			targetID:   "-",
			reason:     ReasonInvocationNotAllowed,
		},
		{
			name:       "short hex agent id",
			content:    "[@Broken](mention://agent/dead-beef) please look",
			targetType: "agent",
			targetID:   "dead-beef",
			reason:     ReasonInvocationNotAllowed,
		},
		{
			name:       "malformed squad id",
			content:    "[@BrokenSquad](mention://squad/-) please look",
			targetType: "squad",
			targetID:   "-",
			reason:     ReasonTargetUnavailable,
		},
		{
			name:       "all plus malformed agent id",
			content:    "[@all](mention://all/all) heads up [@Broken](mention://agent/-) please look",
			targetType: "agent",
			targetID:   "-",
			reason:     ReasonInvocationNotAllowed,
		},
		{
			name:       "all plus malformed squad id",
			content:    "[@all](mention://all/all) heads up [@BrokenSquad](mention://squad/-) please look",
			targetType: "squad",
			targetID:   "-",
			reason:     ReasonTargetUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": tc.content})
			if got := len(preview.Agents); got != 0 {
				t.Fatalf("malformed mention preview agents = %d, want 0: %+v", got, preview.Agents)
			}
			if len(preview.Blocked) != 1 {
				t.Fatalf("malformed mention blocked = %+v, want exactly 1 outcome", preview.Blocked)
			}
			blocked := preview.Blocked[0]
			if blocked.TargetType != tc.targetType || blocked.TargetID != tc.targetID {
				t.Fatalf("blocked target = %s/%s, want %s/%s", blocked.TargetType, blocked.TargetID, tc.targetType, tc.targetID)
			}
			if blocked.Status != DispatchBlocked {
				t.Fatalf("blocked status = %q, want %q", blocked.Status, DispatchBlocked)
			}
			if blocked.ReasonCode != tc.reason {
				t.Fatalf("blocked reason = %q, want %q", blocked.ReasonCode, tc.reason)
			}

			// The create path must survive the same input and enqueue nothing.
			postCommentForTriggerPreviewTest(t, taskID, map[string]any{"content": tc.content})
			if got := countQueuedCommentTriggerRuns(t, taskID, assigneeID); got != 0 {
				t.Fatalf("assignee queued tasks = %d, want 0", got)
			}
		})
	}
}

func TestPreviewCommentTriggers_AllSuppressesAssigneeAndPendingDedupes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "Preview Dedup Assignee", nil)
	taskID := createCommentTriggerPreviewTask(t, "comment trigger all pending", "agent", agentID)

	allPreview := previewCommentTriggersForTest(t, taskID, map[string]any{
		"content": "FYI [@all](mention://all/all)",
	})
	if got := len(allPreview.Agents); got != 0 {
		t.Fatalf("@all preview agents = %d, want 0: %+v", got, allPreview.Agents)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, task_id, status)
		VALUES ($1, $2, $3, 'queued')
	`, agentID, handlerTestRuntimeID(t), taskID); err != nil {
		t.Fatalf("seed queued run: %v", err)
	}

	pendingPreview := previewCommentTriggersForTest(t, taskID, map[string]any{
		"content": "can you continue here?",
	})
	requirePreviewAgents(t, pendingPreview, agentID)
	if pendingPreview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
		t.Fatalf("pending preview source = %q, want %q", pendingPreview.Agents[0].Source, commentTriggerSourceTaskAssignee)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content": "can you continue here?",
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, agentID); got != 1 {
		t.Fatalf("pending assignee create queued tasks = %d, want 1", got)
	}
}

func TestPreviewCommentTriggers_AssignedSquadLeaderAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	leaderID := createHandlerTestAgent(t, "Preview Squad Leader", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Preview Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	taskID := createCommentTriggerPreviewTask(t, "comment trigger squad assignee", "squad", squadID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": "please coordinate this"})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 squad leader preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != leaderID {
		t.Fatalf("preview leader id = %s, want %s", preview.Agents[0].ID, leaderID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceTaskAssignee) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceTaskAssignee)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":            "please coordinate this",
		"suppress_agent_ids": []string{leaderID},
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, leaderID); got != 0 {
		t.Fatalf("suppressed squad leader queued tasks = %d, want 0", got)
	}
}

func TestPreviewCommentTriggers_MentionedSquadLeaderAndSuppress(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	leaderID := createHandlerTestAgent(t, "Preview Mentioned Squad Leader", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, testWorkspaceID, "Preview Mentioned Trigger Squad", leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	taskID := createCommentTriggerPreviewTask(t, "comment trigger mentioned squad", "", "")
	content := fmt.Sprintf("[@Squad](mention://squad/%s) please take this", squadID)

	preview := previewCommentTriggersForTest(t, taskID, map[string]any{"content": content})
	if got := len(preview.Agents); got != 1 {
		t.Fatalf("expected 1 mentioned squad leader preview agent, got %d: %+v", got, preview.Agents)
	}
	if preview.Agents[0].ID != leaderID {
		t.Fatalf("preview leader id = %s, want %s", preview.Agents[0].ID, leaderID)
	}
	if preview.Agents[0].Source != string(commentTriggerSourceMentionSquadLeader) {
		t.Fatalf("preview source = %q, want %q", preview.Agents[0].Source, commentTriggerSourceMentionSquadLeader)
	}

	postCommentForTriggerPreviewTest(t, taskID, map[string]any{
		"content":            content,
		"suppress_agent_ids": []string{leaderID},
	})
	if got := countQueuedCommentTriggerRuns(t, taskID, leaderID); got != 0 {
		t.Fatalf("suppressed mentioned squad leader queued tasks = %d, want 0", got)
	}
}
