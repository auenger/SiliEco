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

func TestListGroupedTasksAssigneePaginatesPerGroup(t *testing.T) {
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	var assigneeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Grouped Tasks Test User", fmt.Sprintf("grouped-%d@silieco.ai", suffix)).Scan(&assigneeID); err != nil {
		t.Fatalf("create assignee user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, assigneeID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, assigneeID); err != nil {
		t.Fatalf("create assignee member: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_runs, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, "Grouped Tasks Test Agent", testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	createTask := func(title, assigneeType, assigneeID string, position float64, startDate *time.Time, stage *int32) string {
		t.Helper()
		var number int32
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET task_counter = GREATEST(
				task_counter,
				(SELECT COALESCE(MAX(number), 0) FROM task WHERE workspace_id = $1)
			) + 1
			WHERE id = $1
			RETURNING task_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next task number: %v", err)
		}

		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO task (
				workspace_id, title, description, status, priority,
				assignee_type, assignee_id, creator_type, creator_id,
				position, number, start_date, stage
			)
			VALUES ($1, $2, NULL, 'todo', 'none', $3, $4, 'member', $5, $6, $7, $8, $9)
			RETURNING id
		`, testWorkspaceID, title, assigneeType, assigneeID, testUserID, position, number, startDate, stage).Scan(&id); err != nil {
			t.Fatalf("create task %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM task WHERE id = $1`, id)
		})
		return id
	}

	stageTwo := int32(2)
	startDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createTask("Grouped member one", "member", assigneeID, 1, &startDate, &stageTwo)
	createTask("Grouped member two", "member", assigneeID, 2, nil, nil)
	createTask("Grouped member three", "member", assigneeID, 3, nil, nil)
	createTask("Grouped agent one", "agent", agentID, 1, nil, nil)

	path := fmt.Sprintf(
		"/api/tasks/grouped?workspace_id=%s&group_by=assignee&statuses=todo&limit=2&assignee_filters=member:%s,agent:%s",
		testWorkspaceID,
		assigneeID,
		agentID,
	)
	w := httptest.NewRecorder()
	testHandler.ListGroupedTasks(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp GroupedTasksResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}

	memberGroupID := "assignee:member:" + assigneeID
	agentGroupID := "assignee:agent:" + agentID
	groups := map[string]TaskAssigneeGroupResponse{}
	for _, group := range resp.Groups {
		groups[group.ID] = group
	}

	memberGroup, ok := groups[memberGroupID]
	if !ok {
		t.Fatalf("missing member group %s in %#v", memberGroupID, resp.Groups)
	}
	if memberGroup.Total != 3 || len(memberGroup.Tasks) != 2 {
		t.Fatalf("member group total/page mismatch: total=%d len=%d", memberGroup.Total, len(memberGroup.Tasks))
	}
	if memberGroup.Tasks[0].Title != "Grouped member one" || memberGroup.Tasks[1].Title != "Grouped member two" {
		t.Fatalf("member group order mismatch: %#v", memberGroup.Tasks)
	}
	if memberGroup.Tasks[0].Stage == nil || *memberGroup.Tasks[0].Stage != stageTwo {
		t.Fatalf("member group first task stage = %#v, want %d", memberGroup.Tasks[0].Stage, stageTwo)
	}
	if memberGroup.Tasks[0].StartDate == nil || *memberGroup.Tasks[0].StartDate != "2026-03-01" {
		t.Fatalf("member group first task start_date = %#v, want 2026-03-01", memberGroup.Tasks[0].StartDate)
	}

	agentGroup, ok := groups[agentGroupID]
	if !ok {
		t.Fatalf("missing agent group %s in %#v", agentGroupID, resp.Groups)
	}
	if agentGroup.Total != 1 || len(agentGroup.Tasks) != 1 {
		t.Fatalf("agent group total/page mismatch: total=%d len=%d", agentGroup.Total, len(agentGroup.Tasks))
	}

	nextPath := fmt.Sprintf(
		"/api/tasks/grouped?workspace_id=%s&group_by=assignee&statuses=todo&limit=2&offset=2&group_assignee_type=member&group_assignee_id=%s",
		testWorkspaceID,
		assigneeID,
	)
	next := httptest.NewRecorder()
	testHandler.ListGroupedTasks(next, newRequest("GET", nextPath, nil))
	if next.Code != http.StatusOK {
		t.Fatalf("ListGroupedTasks next page: expected 200, got %d: %s", next.Code, next.Body.String())
	}

	var nextResp GroupedTasksResponse
	if err := json.NewDecoder(next.Body).Decode(&nextResp); err != nil {
		t.Fatalf("decode next grouped response: %v", err)
	}
	if len(nextResp.Groups) != 1 {
		t.Fatalf("expected one next-page group, got %#v", nextResp.Groups)
	}
	if nextResp.Groups[0].ID != memberGroupID || nextResp.Groups[0].Total != 3 || len(nextResp.Groups[0].Tasks) != 1 {
		t.Fatalf("unexpected next-page group: %#v", nextResp.Groups[0])
	}
	if nextResp.Groups[0].Tasks[0].Title != "Grouped member three" {
		t.Fatalf("unexpected next-page task: %#v", nextResp.Groups[0].Tasks[0])
	}
}
