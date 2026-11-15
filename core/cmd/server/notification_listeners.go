package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/handler"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// mention represents a parsed @mention from markdown content (local alias).
type mention struct {
	Type string // "member", "agent", "task", or "all"
	ID   string // user_id, agent_id, task_id, or "all"
}

// statusLabels maps DB status values to human-readable labels for notifications.
var statusLabels = map[string]string{
	"backlog":     "Backlog",
	"todo":        "Todo",
	"in_progress": "In Progress",
	"in_review":   "In Review",
	"done":        "Done",
	"blocked":     "Blocked",
	"cancelled":   "Cancelled",
}

// priorityLabels maps DB priority values to human-readable labels for notifications.
var priorityLabels = map[string]string{
	"urgent": "Urgent",
	"high":   "High",
	"medium": "Medium",
	"low":    "Low",
	"none":   "No priority",
}

func statusLabel(s string) string {
	if l, ok := statusLabels[s]; ok {
		return l
	}
	return s
}

func priorityLabel(p string) string {
	if l, ok := priorityLabels[p]; ok {
		return l
	}
	return p
}

var emptyDetails = []byte("{}")

// parseMentions extracts mentions from markdown content.
// Delegates to the shared util.ParseMentions and converts to the local type.
func parseMentions(content string) []mention {
	parsed := util.ParseMentions(content)
	result := make([]mention, len(parsed))
	for i, m := range parsed {
		result[i] = mention{Type: m.Type, ID: m.ID}
	}
	return result
}

// parentBubbleNotifTypes is the allowlist of inbox notification types that
// bubble up from a sub-task to subscribers of its parent. Other event types
// only notify subscribers of the sub-task itself, to keep parent watchers'
// inboxes focused on the signal that matters most: status transitions.
var parentBubbleNotifTypes = map[string]bool{
	"status_changed": true,
}

// notifTypeToGroup maps each InboxItemType to a user-configurable preference
// group. Types not in this map are always delivered (not configurable).
var notifTypeToGroup = map[string]string{
	"task_assigned":      "assignments",
	"unassigned":         "assignments",
	"assignee_changed":   "assignments",
	"status_changed":     "status_changes",
	"new_comment":        "comments",
	"mentioned":          "comments",
	"priority_changed":   "updates",
	"start_date_changed": "updates",
	"due_date_changed":   "updates",
	"run_completed":      "agent_activity",
	"run_failed":         "agent_activity",
	"agent_blocked":      "agent_activity",
	"agent_completed":    "agent_activity",
}

// isNotifMuted returns true if the given notification type is muted for a user
// based on their parsed preferences map.
func isNotifMuted(prefs map[string]string, notifType string) bool {
	group, ok := notifTypeToGroup[notifType]
	if !ok {
		return false // unconfigurable types are always delivered
	}
	return prefs[group] == "muted"
}

// loadUserPrefs loads notification preferences for a set of user IDs in a
// workspace. Returns a map from user_id string to parsed preferences.
func loadUserPrefs(
	ctx context.Context,
	queries *db.Queries,
	workspaceID string,
	userIDs []string,
) map[string]map[string]string {
	if len(userIDs) == 0 {
		return nil
	}

	uuids := make([]pgtype.UUID, len(userIDs))
	for i, id := range userIDs {
		uuids[i] = parseUUID(id)
	}

	rows, err := queries.ListNotificationPreferencesByUsers(ctx, db.ListNotificationPreferencesByUsersParams{
		WorkspaceID: parseUUID(workspaceID),
		UserIds:     uuids,
	})
	if err != nil {
		slog.Error("failed to load notification preferences", "error", err)
		return nil
	}

	result := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		var prefs map[string]string
		if err := json.Unmarshal(row.Preferences, &prefs); err != nil {
			continue
		}
		result[util.UUIDToString(row.UserID)] = prefs
	}
	return result
}

// terminalStatusForTaskFailedDismiss is the set of task statuses that mark
// the task as "the user no longer needs to triage past failures." When a
// status change lands on one of these, any pre-existing run_failed inbox
// rows for the task are archived so the inbox stays a fresh-signal surface.
// `in_review` is included because in Silieco's agent flow that's the most
// reliable "work delivered" handoff — and a status flip back to in_progress
// will simply produce new run_failed rows that surface normally.
var terminalStatusForRunFailedDismiss = map[string]bool{
	"in_review": true,
	"done":      true,
	"cancelled": true,
}

// archiveStaleTaskFailedInbox archives all run_failed inbox rows for the
// given task and notifies each affected member recipient via
// inbox:batch-archived so connected clients self-heal.
func archiveStaleRunFailedInbox(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	workspaceID string,
	taskID string,
) {
	rows, err := queries.ArchiveInboxByTaskAndType(ctx, db.ArchiveInboxByTaskAndTypeParams{
		WorkspaceID: parseUUID(workspaceID),
		TaskID:      parseUUID(taskID),
		Type:        "run_failed",
	})
	if err != nil {
		slog.Error("auto-archive run_failed inbox: query failed",
			"workspace_id", workspaceID, "task_id", taskID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Dedupe recipients: the listener creates one row per failure event per
	// subscriber, so a long-running task can yield several rows for the
	// same recipient.
	counts := map[string]int{}
	for _, row := range rows {
		// Inbox rows for run_failed only target member recipients today
		// (notifySubscribers skips agent subscribers), but defend the WS
		// layer against future widening — only members get a personal feed.
		if row.RecipientType != "member" {
			continue
		}
		counts[util.UUIDToString(row.RecipientID)]++
	}

	for recipientID, count := range counts {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxBatchArchived,
			WorkspaceID: workspaceID,
			Payload: map[string]any{
				"recipient_id": recipientID,
				"count":        int64(count),
				"task_id":      taskID,
				"reason":       "task_status_terminal",
			},
		})
	}

	slog.Info("auto-archive run_failed inbox: archived stale rows",
		"workspace_id", workspaceID, "task_id", taskID,
		"row_count", len(rows), "recipient_count", len(counts))
}

// notifySubscribers queries the subscriber table for an task, excludes the
// actor and any extra IDs, and creates inbox items for each remaining member
// subscriber. Publishes an inbox:new event for each notification.
// If the task has a parent and the notification type is in the bubble
// allowlist, parent task subscribers are also notified (deduplicated
// against direct subscribers).
func notifySubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	taskID string,
	taskStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	notified := notifyTaskSubscribers(ctx, queries, bus,
		taskID, taskID, taskStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details)

	// Only a small allowlist of event types bubbles to parent subscribers.
	if !parentBubbleNotifTypes[notifType] {
		return
	}

	// Also notify parent task subscribers if this is a sub-task.
	task, err := queries.GetTask(ctx, parseUUID(taskID))
	if err != nil {
		slog.Error("failed to get task for parent notification",
			"task_id", taskID, "error", err)
		return
	}
	if !task.ParentTaskID.Valid {
		return
	}

	// Merge already-notified IDs into exclude set for parent subscribers.
	parentExclude := make(map[string]bool, len(exclude)+len(notified))
	for id := range exclude {
		parentExclude[id] = true
	}
	for id := range notified {
		parentExclude[id] = true
	}

	// Query subscribers from the parent task, but the inbox item still
	// points to the sub-task so the user navigates to the actual change.
	parentID := util.UUIDToString(task.ParentTaskID)
	notifyTaskSubscribers(ctx, queries, bus,
		parentID, taskID, taskStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, details)
}

// notifyTaskSubscribers sends inbox notifications to subscribers of
// subscriberTaskID, but creates inbox items pointing to targetTaskID.
// This allows querying subscribers from a parent task while the notification
// links to the sub-task where the change actually occurred.
// Returns the set of member IDs that were notified.
func notifyTaskSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	subscriberTaskID string,
	targetTaskID string,
	taskStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) map[string]bool {
	notified := map[string]bool{}

	subs, err := queries.ListTaskSubscribers(ctx, parseUUID(subscriberTaskID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"task_id", subscriberTaskID, "error", err)
		return notified
	}

	// Batch-load notification preferences for all member subscribers.
	var memberIDs []string
	for _, sub := range subs {
		if sub.UserType == "member" {
			memberIDs = append(memberIDs, util.UUIDToString(sub.UserID))
		}
	}
	userPrefs := loadUserPrefs(ctx, queries, workspaceID, memberIDs)

	for _, sub := range subs {
		// Only notify member-type subscribers (not agents)
		if sub.UserType != "member" {
			continue
		}

		subID := util.UUIDToString(sub.UserID)

		// Skip the actor
		if subID == e.ActorID {
			continue
		}

		// Skip any extra excluded IDs
		if exclude[subID] {
			continue
		}

		// Skip if this notification type is muted by the user
		if prefs, ok := userPrefs[subID]; ok && isNotifMuted(prefs, notifType) {
			continue
		}

		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(workspaceID),
			RecipientType: "member",
			RecipientID:   sub.UserID,
			Type:          notifType,
			Severity:      severity,
			TaskID:        parseUUID(targetTaskID),
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("subscriber notification creation failed",
				"subscriber_id", subID, "type", notifType, "error", err)
			continue
		}

		notified[subID] = true
		resp := inboxItemToResponse(item)
		resp["task_status"] = taskStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: workspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}

	return notified
}

// notifyDirect creates an inbox item for a specific recipient. Skips if the
// recipient is the actor. Publishes an inbox:new event on success.
func notifyDirect(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	recipientType string,
	recipientID string,
	workspaceID string,
	e events.Event,
	taskID string,
	taskStatus string,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	// Skip if recipient is the actor
	if recipientID == e.ActorID {
		return
	}

	// Check notification preferences for member recipients.
	if recipientType == "member" {
		prefs := loadUserPrefs(ctx, queries, workspaceID, []string{recipientID})
		if p, ok := prefs[recipientID]; ok && isNotifMuted(p, notifType) {
			return
		}
	}

	item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: recipientType,
		RecipientID:   parseUUID(recipientID),
		Type:          notifType,
		Severity:      severity,
		TaskID:        parseUUID(taskID),
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText(e.ActorType),
		ActorID:       optionalUUID(e.ActorID),
		Details:       details,
	})
	if err != nil {
		slog.Error("direct notification creation failed",
			"recipient_id", recipientID, "type", notifType, "error", err)
		return
	}

	resp := inboxItemToResponse(item)
	resp["task_status"] = taskStatus
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		Payload:     map[string]any{"item": resp},
	})
}

// notifyMentionedMembers creates inbox items for each @mentioned member,
// excluding the actor and any IDs in the skip set. When an @all mention is
// present, all workspace members are notified (excluding agents).
func notifyMentionedMembers(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	mentions []mention,
	taskID string,
	taskTitle string,
	taskStatus string,
	title string,
	skip map[string]bool,
	details []byte,
) {
	// Collect the set of member IDs to notify.
	recipientIDs := map[string]bool{}

	hasAll := false
	var squadIDs []string
	for _, m := range mentions {
		if m.Type == "all" {
			hasAll = true
			continue
		}
		if m.Type == "member" {
			recipientIDs[m.ID] = true
		}
		if m.Type == "squad" {
			squadIDs = append(squadIDs, m.ID)
		}
	}

	// Expand each @squad mention to its human members. Agent members of a
	// squad are reached via comment-trigger / assignment paths, not the
	// mention-inbox path, so we only seed member-typed recipients here.
	for _, sid := range squadIDs {
		squadUUID, err := util.ParseUUID(sid)
		if err != nil {
			continue
		}
		members, err := queries.ListSquadMembers(context.Background(), squadUUID)
		if err != nil {
			slog.Error("failed to list squad members for @squad mention", "squad_id", sid, "error", err)
			continue
		}
		for _, sm := range members {
			if sm.MemberType == "member" {
				recipientIDs[util.UUIDToString(sm.MemberID)] = true
			}
		}
	}

	// If @all is present, expand to all workspace members.
	if hasAll {
		members, err := queries.ListMembers(context.Background(), parseUUID(e.WorkspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", e.WorkspaceID, "error", err)
		} else {
			for _, m := range members {
				recipientIDs[util.UUIDToString(m.UserID)] = true
			}
		}
	}

	// Batch-load notification preferences for all mention recipients.
	var mentionUserIDs []string
	for id := range recipientIDs {
		if id != e.ActorID && !skip[id] {
			mentionUserIDs = append(mentionUserIDs, id)
		}
	}
	mentionPrefs := loadUserPrefs(context.Background(), queries, e.WorkspaceID, mentionUserIDs)

	for id := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		// Skip if mentions/comments are muted by this user
		if p, ok := mentionPrefs[id]; ok && isNotifMuted(p, "mentioned") {
			continue
		}
		item, err := queries.CreateInboxItem(context.Background(), db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(id),
			Type:          "mentioned",
			Severity:      "info",
			TaskID:        parseUUID(taskID),
			Title:         title,
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("mention inbox creation failed", "mentioned_id", id, "error", err)
			continue
		}
		resp := inboxItemToResponse(item)
		resp["task_status"] = taskStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: e.WorkspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}
}

// registerNotificationListeners wires up event bus listeners that create inbox
// notifications using the subscriber table. This replaces the old hardcoded
// notification logic from inbox_listeners.go.
//
// NOTE: uses context.Background() because the event bus dispatches synchronously
// within the HTTP request goroutine. Adding per-handler timeouts is a bus-level
// concern — see events.Bus for future improvements.
func registerNotificationListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// task:created — Direct notification to assignee if assignee != actor
	bus.Subscribe(protocol.EventTaskCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		task, ok := payload["task"].(handler.TaskResponse)
		if !ok {
			return
		}

		// Track who already got notified to avoid duplicates
		skip := map[string]bool{e.ActorID: true}

		// Direct notification to assignee
		if task.AssigneeType != nil && task.AssigneeID != nil {
			skip[*task.AssigneeID] = true
			notifyDirect(ctx, queries, bus,
				*task.AssigneeType, *task.AssigneeID,
				task.WorkspaceID, e, task.ID, task.Status,
				"task_assigned", "action_required",
				task.Title,
				"",
				emptyDetails,
			)
		}

		// Notify @mentions in description
		if task.Description != nil && *task.Description != "" {
			mentions := parseMentions(*task.Description)
			notifyMentionedMembers(bus, queries, e, mentions, task.ID, task.Title, task.Status,
				task.Title, skip, emptyDetails)
		}
	})

	// task:updated — handle assignee changes, status changes, priority, due date
	bus.Subscribe(protocol.EventTaskUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		task, ok := payload["task"].(handler.TaskResponse)
		if !ok {
			return
		}
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		statusChanged, _ := payload["status_changed"].(bool)
		descriptionChanged, _ := payload["description_changed"].(bool)
		prevAssigneeType, _ := payload["prev_assignee_type"].(*string)
		prevAssigneeID, _ := payload["prev_assignee_id"].(*string)
		prevDescription, _ := payload["prev_description"].(*string)

		if assigneeChanged {
			// Build structured details for assignee change
			detailsMap := map[string]any{}
			if prevAssigneeType != nil {
				detailsMap["prev_assignee_type"] = *prevAssigneeType
			}
			if prevAssigneeID != nil {
				detailsMap["prev_assignee_id"] = *prevAssigneeID
			}
			if task.AssigneeType != nil {
				detailsMap["new_assignee_type"] = *task.AssigneeType
			}
			if task.AssigneeID != nil {
				detailsMap["new_assignee_id"] = *task.AssigneeID
			}
			assigneeDetails, _ := json.Marshal(detailsMap)

			// Direct: notify new assignee about assignment
			if task.AssigneeType != nil && task.AssigneeID != nil {
				notifyDirect(ctx, queries, bus,
					*task.AssigneeType, *task.AssigneeID,
					e.WorkspaceID, e, task.ID, task.Status,
					"task_assigned", "action_required",
					task.Title,
					"",
					assigneeDetails,
				)
			}

			// Direct: notify old assignee about unassignment
			if prevAssigneeType != nil && prevAssigneeID != nil && *prevAssigneeType == "member" {
				notifyDirect(ctx, queries, bus,
					"member", *prevAssigneeID,
					e.WorkspaceID, e, task.ID, task.Status,
					"unassigned", "info",
					task.Title,
					"",
					assigneeDetails,
				)
			}

			// Subscriber: notify remaining subscribers about assignee change,
			// excluding actor, old assignee, and new assignee
			exclude := map[string]bool{}
			if prevAssigneeID != nil {
				exclude[*prevAssigneeID] = true
			}
			if task.AssigneeID != nil {
				exclude[*task.AssigneeID] = true
			}
			notifySubscribers(ctx, queries, bus, task.ID, task.Status, e.WorkspaceID, e,
				exclude, "assignee_changed", "info",
				task.Title, "",
				assigneeDetails)
		}

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			statusDetails, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   task.Status,
			})
			notifySubscribers(ctx, queries, bus, task.ID, task.Status, e.WorkspaceID, e,
				nil, "status_changed", "info",
				task.Title, "",
				statusDetails)

			// When the task progresses past the failure (in_review / done /
			// cancelled), retire any stale run_failed inbox rows so the
			// inbox reflects the current state of the work, not its history.
			// The activity log keeps the full failure history for audit.
			if terminalStatusForRunFailedDismiss[task.Status] {
				archiveStaleRunFailedInbox(ctx, queries, bus, e.WorkspaceID, task.ID)
			}
		}

		if priorityChanged, _ := payload["priority_changed"].(bool); priorityChanged {
			prevPriority, _ := payload["prev_priority"].(string)
			priorityDetails, _ := json.Marshal(map[string]string{
				"from": prevPriority,
				"to":   task.Priority,
			})
			notifySubscribers(ctx, queries, bus, task.ID, task.Status, e.WorkspaceID, e,
				nil, "priority_changed", "info",
				task.Title, "",
				priorityDetails)
		}

		if startDateChanged, _ := payload["start_date_changed"].(bool); startDateChanged {
			prevStartDateStr := ""
			if prevStartDate, ok := payload["prev_start_date"].(*string); ok && prevStartDate != nil {
				prevStartDateStr = *prevStartDate
			}
			newStartDateStr := ""
			if task.StartDate != nil {
				newStartDateStr = *task.StartDate
			}
			startDateDetails, _ := json.Marshal(map[string]string{
				"from": prevStartDateStr,
				"to":   newStartDateStr,
			})
			notifySubscribers(ctx, queries, bus, task.ID, task.Status, e.WorkspaceID, e,
				nil, "start_date_changed", "info",
				task.Title, "",
				startDateDetails)
		}

		if dueDateChanged, _ := payload["due_date_changed"].(bool); dueDateChanged {
			prevDueDateStr := ""
			if prevDueDate, ok := payload["prev_due_date"].(*string); ok && prevDueDate != nil {
				prevDueDateStr = *prevDueDate
			}
			newDueDateStr := ""
			if task.DueDate != nil {
				newDueDateStr = *task.DueDate
			}
			dueDateDetails, _ := json.Marshal(map[string]string{
				"from": prevDueDateStr,
				"to":   newDueDateStr,
			})
			notifySubscribers(ctx, queries, bus, task.ID, task.Status, e.WorkspaceID, e,
				nil, "due_date_changed", "info",
				task.Title, "",
				dueDateDetails)
		}

		// Notify NEW @mentions in description
		if descriptionChanged && task.Description != nil {
			newMentions := parseMentions(*task.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				var added []mention
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						added = append(added, m)
					}
				}
				skip := map[string]bool{e.ActorID: true}
				notifyMentionedMembers(bus, queries, e, added, task.ID, task.Title, task.Status,
					task.Title, skip, emptyDetails)
			}
		}
	})

	// comment:created — notify all subscribers except the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// The comment payload can come as handler.CommentResponse from the
		// HTTP handler, or as map[string]any from the agent comment path in
		// task.go. Handle both.
		var taskID, commentID, commentContent, authorType string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			taskID = c.TaskID
			commentID = c.ID
			commentContent = c.Content
			authorType = c.AuthorType
		case map[string]any:
			taskID, _ = c["task_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
			authorType, _ = c["author_type"].(string)
		default:
			return
		}

		// Platform-authored system comments (SILI-2538 child-done parent
		// notify) must NOT create inbox rows or parse mentions from their
		// body — the comment is a controlled platform signal, not a human
		// commenter. Mention parsing is the dangerous bit: if the body
		// transcluded a child title containing `mention://member/<uuid>`,
		// the parent's assignee inbox would light up via the generic path.
		// Skip the listener entirely; the WS broadcast still delivers the
		// comment to the task timeline.
		if authorType == "system" {
			return
		}

		taskTitle, _ := payload["task_title"].(string)
		taskStatus, _ := payload["task_status"].(string)

		commentDetails := emptyDetails
		if commentID != "" {
			commentDetails, _ = json.Marshal(map[string]string{
				"comment_id": commentID,
			})
		}

		notifySubscribers(ctx, queries, bus, taskID, taskStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			taskTitle, commentContent,
			commentDetails)

		// Notify @mentions in comment content.
		mentions := parseMentions(commentContent)
		if len(mentions) > 0 {
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMembers(bus, queries, e, mentions, taskID, taskTitle, taskStatus,
				taskTitle, skip, commentDetails)
		}
	})

	// task_reaction:added — notify the task creator
	bus.Subscribe(protocol.EventTaskReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.TaskReactionResponse)
		if !ok {
			return
		}

		creatorType, _ := payload["creator_type"].(string)
		creatorID, _ := payload["creator_id"].(string)
		taskID, _ := payload["task_id"].(string)
		taskTitle, _ := payload["task_title"].(string)
		taskStatus, _ := payload["task_status"].(string)

		if creatorType == "" || creatorID == "" {
			return
		}

		details, _ := json.Marshal(map[string]string{
			"emoji": reaction.Emoji,
		})

		notifyDirect(ctx, queries, bus,
			creatorType, creatorID,
			e.WorkspaceID, e, taskID, taskStatus,
			"reaction_added", "info",
			taskTitle, "",
			details,
		)
	})

	// reaction:added — notify the comment author
	bus.Subscribe(protocol.EventReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.ReactionResponse)
		if !ok {
			return
		}

		commentAuthorType, _ := payload["comment_author_type"].(string)
		commentAuthorID, _ := payload["comment_author_id"].(string)
		commentID, _ := payload["comment_id"].(string)
		taskID, _ := payload["task_id"].(string)
		taskTitle, _ := payload["task_title"].(string)
		taskStatus, _ := payload["task_status"].(string)

		if commentAuthorType == "" || commentAuthorID == "" {
			return
		}

		detailsMap := map[string]string{
			"emoji": reaction.Emoji,
		}
		if commentID != "" {
			detailsMap["comment_id"] = commentID
		}
		details, _ := json.Marshal(detailsMap)

		notifyDirect(ctx, queries, bus,
			commentAuthorType, commentAuthorID,
			e.WorkspaceID, e, taskID, taskStatus,
			"reaction_added", "info",
			taskTitle, "",
			details,
		)
	})

	// run:completed — no inbox notification (completion is visible from status change)

	// run:failed — notify all subscribers except the agent
	bus.Subscribe(protocol.EventRunFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		taskID, _ := payload["task_id"].(string)
		if taskID == "" {
			return
		}

		task, err := queries.GetTask(ctx, parseUUID(taskID))
		if err != nil {
			slog.Error("run:failed notification: failed to get task", "task_id", taskID, "error", err)
			return
		}

		exclude := map[string]bool{}
		if agentID != "" {
			exclude[agentID] = true
		}

		notifySubscribers(ctx, queries, bus, taskID, task.Status, e.WorkspaceID,
			events.Event{
				Type:        e.Type,
				WorkspaceID: e.WorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
			},
			exclude, "run_failed", "action_required",
			task.Title, "",
			emptyDetails)
	})
}

// inboxItemToResponse converts a db.InboxItem into a map suitable for
// JSON-serializable event payloads (mirrors handler.inboxToResponse fields).
func inboxItemToResponse(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"task_id":        util.UUIDToPtr(item.TaskID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
}
