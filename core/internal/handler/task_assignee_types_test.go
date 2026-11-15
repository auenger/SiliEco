package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Backs the workspace Members/Agents tabs: assignee_types on ListTasks must
// filter server-side (same semantics as the ListGroupedTasks param) so the
// client no longer post-filters loaded pages, and `total` must agree with the
// filter so per-status pagination counts stay correct.
func TestListTasks_AssigneeTypesFilter(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	// Dedicated project so counts aren't polluted by other tests sharing the
	// workspace.
	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Assignee Types %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Assignee Types Agent %d", suffix), testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	insertTask := func(title string, assigneeType, assigneeID *string) string {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET task_counter = GREATEST(task_counter, (SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING task_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next task number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO task (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'none', $3, $4, 'member', $5, 0, $6, $7) RETURNING id
		`, testWorkspaceID, title, assigneeType, assigneeID, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create task %q: %v", title, err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, id) })
		return id
	}

	memberType, agentType := "member", "agent"
	memberTask := insertTask(fmt.Sprintf("at-member-%d", suffix), &memberType, &testUserID)
	taskAssignedToAgent := insertTask(fmt.Sprintf("at-agent-%d", suffix), &agentType, &agentID)
	unassignedTask := insertTask(fmt.Sprintf("at-none-%d", suffix), nil, nil)

	list := func(query string) (ids []string, total int64) {
		path := fmt.Sprintf("/api/tasks?workspace_id=%s&project_id=%s&limit=500%s",
			testWorkspaceID, projectID, query)
		w := httptest.NewRecorder()
		testHandler.ListTasks(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListTasks: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Tasks []TaskResponse `json:"tasks"`
			Total int64          `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		for _, iss := range resp.Tasks {
			ids = append(ids, iss.ID)
		}
		return ids, resp.Total
	}

	// Baseline: every project task comes back without the filter.
	allIDs, allTotal := list("")
	for _, want := range []string{memberTask, taskAssignedToAgent, unassignedTask} {
		if !containsTaskID(allIDs, want) {
			t.Fatalf("baseline list missing %s — all=%v", want, allIDs)
		}
	}
	if allTotal != 3 {
		t.Fatalf("baseline total: want 3, got %d", allTotal)
	}

	// Members tab: only member-assigned tasks, and total agrees.
	memberIDs, memberTotal := list("&assignee_types=member")
	if !containsTaskID(memberIDs, memberTask) {
		t.Fatalf("member filter missing %s — got %v", memberTask, memberIDs)
	}
	if containsTaskID(memberIDs, taskAssignedToAgent) || containsTaskID(memberIDs, unassignedTask) {
		t.Fatalf("member filter leaked non-member tasks: %v", memberIDs)
	}
	if memberTotal != 1 {
		t.Fatalf("member total: want 1, got %d", memberTotal)
	}

	// Agents tab: agent+squad kinds — squad has no rows here, param must
	// still parse and return the agent-assigned task only.
	agentIDs, agentTotal := list("&assignee_types=agent,squad")
	if !containsTaskID(agentIDs, taskAssignedToAgent) {
		t.Fatalf("agent filter missing %s — got %v", taskAssignedToAgent, agentIDs)
	}
	if containsTaskID(agentIDs, memberTask) || containsTaskID(agentIDs, unassignedTask) {
		t.Fatalf("agent filter leaked non-agent tasks: %v", agentIDs)
	}
	if agentTotal != 1 {
		t.Fatalf("agent total: want 1, got %d", agentTotal)
	}

	// Unknown actor kinds are a client bug — reject, don't coerce.
	bad := httptest.NewRecorder()
	testHandler.ListTasks(bad, newRequest("GET", fmt.Sprintf(
		"/api/tasks?workspace_id=%s&assignee_types=bogus", testWorkspaceID), nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid assignee_types: expected 400, got %d: %s", bad.Code, bad.Body.String())
	}
}
