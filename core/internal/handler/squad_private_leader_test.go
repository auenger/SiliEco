package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateTask_SquadPrivateLeader_PlainMemberBlocked verifies that a
// plain member cannot create an task assigned to a squad whose leader is
// a private agent.
func TestCreateTask_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Create Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Should be blocked",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTask_SquadPrivateLeader_PlainMemberBlocked verifies that a
// plain member cannot update an task's assignee to a private-leader squad.
func TestUpdateTask_SquadPrivateLeader_PlainMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Update Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create an unassigned task as workspace owner.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title)
		VALUES ($1, 'member', $2, 'update target')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "PATCH", "/api/tasks/"+taskID, map[string]any{
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	r = withURLParam(r, "id", taskID)
	testHandler.UpdateTask(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTask_SquadPrivateLeader_OwnerAllowed verifies that a workspace
// owner CAN assign an task to a squad with a private leader.
func TestCreateTask_SquadPrivateLeader_OwnerAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Owner Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// The AGENT OWNER assigns — allowed. (SILI-3963: workspace owner/admin no
	// longer bypasses a private leader's invocation gate.)
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Owner assigns private-leader squad",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, created.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, created.ID)
	})
}

// TestComment_SquadPrivateLeader_PlainMemberNoEnqueue verifies that a plain
// member posting a comment on an task assigned to a private-leader squad
// does NOT trigger the leader.
func TestComment_SquadPrivateLeader_PlainMemberNoEnqueue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, _, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Comment Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create task assigned to the squad as workspace owner.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private leader comment test', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Plain member posts a plain comment (not a @mention).
	w := httptest.NewRecorder()
	r := newRequestAs(memberID, "POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "any update on this?",
	})
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader must NOT have a queued task.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		taskID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("private leader got %d queued tasks from plain member comment; want 0", count)
	}
}

// TestChildDone_SquadPrivateLeader_PlainMemberWakesLeader verifies that when
// a plain member completes a child task whose parent is assigned to a
// private-leader squad, the leader IS woken. Child-done no longer re-checks
// leader invocation permission (SILI-4063 / GH #4928): the parent was already
// assigned to the squad — which passed the invocation gate — so waking that
// squad's own leader to advance the next stage is a coordination handoff, not
// a fresh invocation. This mirrors the ungated agent-parent path
// (triggerChildDoneAgent); agent and squad child-done now follow one path.
func TestChildDone_SquadPrivateLeader_PlainMemberWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, memberID := privateAgentTestFixture(t)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader ChildDone Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create parent task assigned to the squad (as the AGENT OWNER, who is
	// allowed to invoke the private leader under SILI-3963).
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "parent with private-leader squad",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE parent_task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, parent.ID)
	})

	// Clear any tasks enqueued by the create.
	testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, parent.ID)

	// Create a child task via API (as workspace owner, with member assignee).
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "child task",
		"parent_task_id": parent.ID,
		"assignee_type":  "member",
		"assignee_id":    memberID,
		"status":         "in_progress",
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, child.ID)
	})

	// Plain member moves child to done.
	w = httptest.NewRecorder()
	r = newRequestAs(memberID, "PATCH", "/api/tasks/"+child.ID, map[string]any{
		"status": "done",
	})
	r = withURLParam(r, "id", child.ID)
	testHandler.UpdateTask(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask (child done): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader MUST have a queued task on the parent — child-done
	// wakes the parent's own leader regardless of who closed the child.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		parent.ID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks from plain member child-done; want >=1")
	}
}

// TestChildDone_SquadPrivateLeader_AgentActorWakesLeader is the core SILI-4063
// regression: an AGENT (a squad worker) closes a child under a private-leader
// squad parent, and the child's completing agent has NO human originator who
// could invoke the private leader. This is the exact process-squad pipeline
// shape that used to strand — the removed canEnqueueSquadLeader gate failed
// closed here — while a direct-to-leader-agent parent advanced fine. The
// leader must now be woken.
func TestChildDone_SquadPrivateLeader_AgentActorWakesLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, memberID := privateAgentTestFixture(t)
	workerAgentID := createHandlerTestAgent(t, "squad-private-leader-childdone-worker", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader ChildDone AgentActor Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Parent assigned to the squad by the agent owner (allowed under SILI-3963).
	w := httptest.NewRecorder()
	r := newRequestAs(ownerID, "POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "parent with private-leader squad (agent child-done)",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE parent_task_id = $1`, parent.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, parent.ID)
	})

	// Clear the leader task the assign enqueued so the child-done wake is the
	// only thing that can create one.
	testPool.Exec(ctx, `DELETE FROM agent_run WHERE task_id = $1`, parent.ID)

	// Child assigned to the worker agent, in progress.
	w = httptest.NewRecorder()
	r = newRequest("POST", "/api/tasks?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "child task worked by an agent",
		"parent_task_id": parent.ID,
		"assignee_type":  "agent",
		"assignee_id":    workerAgentID,
		"status":         "in_progress",
	})
	testHandler.CreateTask(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child TaskResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, child.ID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, child.ID)
	})

	// The worker agent's running task on the CHILD. Its originator is the plain
	// member — who cannot invoke the private leader — proving the wake no longer
	// depends on the completer being able to invoke the leader.
	var workerRunID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $3, $3)
		RETURNING id
	`, workerAgentID, child.ID, memberID).Scan(&workerRunID); err != nil {
		t.Fatalf("create worker run: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, workerRunID)
	})

	// Worker agent moves the child to done (agent actor via X-Agent-ID/X-Run-ID).
	w = httptest.NewRecorder()
	r = newRequest("PATCH", "/api/tasks/"+child.ID, map[string]any{
		"status": "done",
	})
	r.Header.Set("X-Agent-ID", workerAgentID)
	r.Header.Set("X-Run-ID", workerRunID)
	r = withURLParam(r, "id", child.ID)
	testHandler.UpdateTask(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateTask (child done): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader MUST have a queued task on the parent.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		parent.ID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks from agent child-done; want >=1 (SILI-4063)")
	}
}

// TestComment_SquadPrivateLeader_AgentActorAllowed verifies that an agent
// actor CAN explicitly trigger the private leader via a squad mention.
func TestComment_SquadPrivateLeader_AgentActorAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, ownerID, _ := privateAgentTestFixture(t)
	otherAgentID := createHandlerTestAgent(t, "squad-private-leader-agent-actor", nil)

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Private Leader Agent Actor Test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID)
	})

	// Create task assigned to the squad.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO task (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id)
		VALUES ($1, 'member', $2, 'private leader agent actor test', 'squad', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, squadID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE task_id = $1`, taskID)
		testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, taskID)
	})

	// Create a task for the other agent whose top-of-chain originator is the
	// private leader's OWNER. Under SILI-3963 A2A is judged by that originator,
	// so the agent-actor squad mention resolves to the owner and may invoke
	// the private leader.
	var runID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_run (agent_id, runtime_id, status, priority, task_id, originator_user_id, accountable_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2, $3, $3)
		RETURNING id
	`, otherAgentID, taskID, ownerID).Scan(&runID); err != nil {
		t.Fatalf("create agent run: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, runID)
	})

	// Agent posts a comment with an explicit squad mention.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/tasks/"+taskID+"/comments", map[string]any{
		"content": "[@Squad](mention://squad/" + squadID + ") agent reporting in",
	})
	r.Header.Set("X-Agent-ID", otherAgentID)
	r.Header.Set("X-Run-ID", runID)
	r = withURLParam(r, "id", taskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// The private leader SHOULD have a queued task — the agent-actor mention's
	// top-of-chain originator is the leader's owner, so A2A-by-originator
	// admits it (SILI-3963).
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status = 'queued'`,
		taskID, agentID,
	).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count == 0 {
		t.Fatalf("private leader got 0 queued tasks; want >=1 (A2A originator is the leader owner)")
	}
}
