package main

import (
	"context"
	"log/slog"

	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/handler"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// registerSubscriberListeners wires up event bus listeners that auto-subscribe
// relevant users to tasks. This ensures creators, assignees, and commenters
// are automatically tracked as task subscribers.
func registerSubscriberListeners(bus *events.Bus, queries *db.Queries) {
	// task:created — subscribe creator + assignee (if different)
	bus.Subscribe(protocol.EventTaskCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Tasks created via handler use TaskResponse; autopilot-created tasks
		// use map[string]any (see service/autopilot.go → issueToMap).
		task, ok := extractTaskFields(payload["task"])
		if !ok {
			return
		}

		// Subscribe the creator
		addSubscriber(bus, queries, e.WorkspaceID, task.ID, task.CreatorType, task.CreatorID, "creator")

		// Subscribe the assignee if exists and different from creator
		if task.AssigneeType != nil && task.AssigneeID != nil &&
			!(*task.AssigneeType == task.CreatorType && *task.AssigneeID == task.CreatorID) {
			addSubscriber(bus, queries, e.WorkspaceID, task.ID, *task.AssigneeType, *task.AssigneeID, "assignee")
		}

		// Subscribe @mentioned users in description
		if task.Description != nil && *task.Description != "" {
			for _, m := range parseMentions(*task.Description) {
				addSubscriber(bus, queries, e.WorkspaceID, task.ID, m.Type, m.ID, "mentioned")
			}
		}
	})

	// task:updated — subscribe new assignee or @mentioned users
	bus.Subscribe(protocol.EventTaskUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		task, ok := extractTaskFields(payload["task"])
		if !ok {
			return
		}

		// Subscribe new assignee if assignee changed
		if assigneeChanged, _ := payload["assignee_changed"].(bool); assigneeChanged {
			if task.AssigneeType != nil && task.AssigneeID != nil {
				addSubscriber(bus, queries, e.WorkspaceID, task.ID, *task.AssigneeType, *task.AssigneeID, "assignee")
			}
		}

		// Subscribe newly @mentioned users in description
		if descriptionChanged, _ := payload["description_changed"].(bool); descriptionChanged && task.Description != nil {
			newMentions := parseMentions(*task.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription, _ := payload["prev_description"].(*string); prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						addSubscriber(bus, queries, e.WorkspaceID, task.ID, m.Type, m.ID, "mentioned")
					}
				}
			}
		}
	})

	// comment:created — subscribe the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// Comments created via handler use CommentResponse; agent comments from task.go use map[string]any
		var taskID, authorType, authorID string
		if comment, ok := payload["comment"].(handler.CommentResponse); ok {
			taskID = comment.TaskID
			authorType = comment.AuthorType
			authorID = comment.AuthorID
		} else if commentMap, ok := payload["comment"].(map[string]any); ok {
			taskID, _ = commentMap["task_id"].(string)
			authorType, _ = commentMap["author_type"].(string)
			authorID, _ = commentMap["author_id"].(string)
		} else {
			return
		}
		if taskID == "" || authorID == "" {
			return
		}

		// Platform-authored system comments (SILI-2538 child-done parent notify)
		// have author_type='system' and a zero UUID author. They must NOT
		// add a subscriber row: task_subscriber.user_type is constrained to
		// ('member','agent'), and a "system" subscriber has no inbox to read
		// anyway. Skip them at the side-effect boundary so the system event
		// stays a pure WS broadcast for the timeline.
		if authorType == "system" {
			return
		}

		addSubscriber(bus, queries, e.WorkspaceID, taskID, authorType, authorID, "commenter")
	})
}

// extractTaskFields normalizes an task payload that may be either a
// handler.TaskResponse struct (HTTP handler path) or a map[string]any
// (autopilot service path) into a common shape.
func extractTaskFields(v any) (handler.TaskResponse, bool) {
	if task, ok := v.(handler.TaskResponse); ok {
		return task, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return handler.TaskResponse{}, false
	}
	task := handler.TaskResponse{}
	task.ID, _ = m["id"].(string)
	task.WorkspaceID, _ = m["workspace_id"].(string)
	task.CreatorType, _ = m["creator_type"].(string)
	task.CreatorID, _ = m["creator_id"].(string)
	task.AssigneeType, _ = m["assignee_type"].(*string)
	task.AssigneeID, _ = m["assignee_id"].(*string)
	task.Description, _ = m["description"].(*string)
	if task.ID == "" || task.CreatorID == "" {
		return handler.TaskResponse{}, false
	}
	return task, true
}

// addSubscriber adds a user as an task subscriber and publishes a
// subscriber:added event for real-time frontend sync.
func addSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, taskID, userType, userID, reason string) {
	err := queries.AddTaskSubscriber(context.Background(), db.AddTaskSubscriberParams{
		TaskID:   parseUUID(taskID),
		UserType: userType,
		UserID:   parseUUID(userID),
		Reason:   reason,
	})
	if err != nil {
		slog.Error("failed to add task subscriber",
			"task_id", taskID,
			"user_type", userType,
			"user_id", userID,
			"reason", reason,
			"error", err,
		)
		return
	}

	bus.Publish(events.Event{
		Type:        protocol.EventSubscriberAdded,
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"task_id":   taskID,
			"user_type": userType,
			"user_id":   userID,
			"reason":    reason,
		},
	})
}
