package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/handler"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// listActivitiesForTask is a test helper that fetches up to 100 activity_log
// records for an task. Uses the same query that backs the timeline endpoint.
func listActivitiesForTask(t *testing.T, queries *db.Queries, taskID string) []db.ActivityLog {
	t.Helper()
	activities, err := queries.ListActivitiesForTask(context.Background(), db.ListActivitiesForTaskParams{
		TaskID: util.MustParseUUID(taskID),
		Limit:  100,
	})
	if err != nil {
		t.Fatalf("ListActivitiesForTask: %v", err)
	}
	return activities
}

func cleanupActivities(t *testing.T, taskID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM activity_log WHERE task_id = $1`, taskID)
}

func TestActivityTaskCreated(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"task": handler.TaskResponse{
				ID:          taskID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test task",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "created" {
		t.Fatalf("expected action 'created', got %q", activities[0].Action)
	}
	if util.UUIDToString(activities[0].ActorID) != testUserID {
		t.Fatalf("expected actor_id %s, got %s", testUserID, util.UUIDToString(activities[0].ActorID))
	}
}

func TestActivityTaskUpdated_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventTaskUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"task": handler.TaskResponse{
				ID:          taskID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test task",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "status_changed" {
		t.Fatalf("expected action 'status_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["from"] != "todo" {
		t.Fatalf("expected from 'todo', got %q", details["from"])
	}
	if details["to"] != "in_progress" {
		t.Fatalf("expected to 'in_progress', got %q", details["to"])
	}
}

func TestActivityTaskUpdated_AssigneeChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	assigneeEmail := "activity-assignee-test@silieco.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventTaskUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"task": handler.TaskResponse{
				ID:           taskID,
				WorkspaceID:  testWorkspaceID,
				Title:        "activity test task",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
			"assignee_changed":   true,
			"prev_assignee_type": (*string)(nil),
			"prev_assignee_id":   (*string)(nil),
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "assignee_changed" {
		t.Fatalf("expected action 'assignee_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["to_type"] != "member" {
		t.Fatalf("expected to_type 'member', got %q", details["to_type"])
	}
	if details["to_id"] != assigneeID {
		t.Fatalf("expected to_id %q, got %q", assigneeID, details["to_id"])
	}
}

func TestActivityTaskUpdated_NoChangeFlags(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	// Publish task:updated with no change flags set
	bus.Publish(events.Event{
		Type:        protocol.EventTaskUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"task": handler.TaskResponse{
				ID:          taskID,
				WorkspaceID: testWorkspaceID,
				Title:       "activity test task",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed":    false,
			"status_changed":      false,
			"description_changed": false,
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 0 {
		t.Fatalf("expected 0 activities when no change flags, got %d", len(activities))
	}
}

func TestActivityTaskUpdated_TitleChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventTaskUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"task": handler.TaskResponse{
				ID:          taskID,
				WorkspaceID: testWorkspaceID,
				Title:       "renamed task",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"title_changed": true,
			"prev_title":    "activity test task",
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "title_changed" {
		t.Fatalf("expected action 'title_changed', got %q", activities[0].Action)
	}

	var details map[string]string
	if err := json.Unmarshal(activities[0].Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["from"] != "activity test task" {
		t.Fatalf("expected from 'activity test task', got %q", details["from"])
	}
	if details["to"] != "renamed task" {
		t.Fatalf("expected to 'renamed task', got %q", details["to"])
	}
}

func TestActivityRunCompleted(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	agentID := testUserID // reuse as a stand-in for agent ID

	bus.Publish(events.Event{
		Type:        protocol.EventRunCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"run_id":   "00000000-0000-0000-0000-000000000001",
			"agent_id": agentID,
			"task_id":  taskID,
			"status":   "completed",
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "run_completed" {
		t.Fatalf("expected action 'run_completed', got %q", activities[0].Action)
	}
	if util.UUIDToString(activities[0].ActorID) != agentID {
		t.Fatalf("expected actor_id %s, got %s", agentID, util.UUIDToString(activities[0].ActorID))
	}
}

func TestActivityRunFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerActivityListeners(bus, queries)

	taskID := createTestTask(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupActivities(t, taskID)
		cleanupTestTask(t, taskID)
	})

	agentID := testUserID

	bus.Publish(events.Event{
		Type:        protocol.EventRunFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"run_id":   "00000000-0000-0000-0000-000000000002",
			"agent_id": agentID,
			"task_id":  taskID,
			"status":   "failed",
		},
	})

	activities := listActivitiesForTask(t, queries, taskID)
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Action != "run_failed" {
		t.Fatalf("expected action 'run_failed', got %q", activities[0].Action)
	}
}
