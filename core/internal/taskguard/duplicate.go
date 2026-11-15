package taskguard

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func NormalizeTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

func DuplicateMessage(identifier, title, status string) string {
	return "Active duplicate task exists: " + identifier + " " + title + " (status: " + status + "). Set allow_duplicate=true or use --allow-duplicate to create another."
}

type ActiveDuplicateError struct {
	ID         string
	Identifier string
	Title      string
	Status     string
}

func (e *ActiveDuplicateError) Error() string {
	return DuplicateMessage(e.Identifier, e.Title, e.Status)
}

func NewActiveDuplicateError(task db.Task, taskPrefix string) *ActiveDuplicateError {
	return &ActiveDuplicateError{
		ID:         util.UUIDToString(task.ID),
		Identifier: fmt.Sprintf("%s-%d", taskPrefix, task.Number),
		Title:      task.Title,
		Status:     task.Status,
	}
}

func LockAndFindActiveDuplicate(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	parentTaskID pgtype.UUID,
	title string,
	allowDuplicate bool,
) (db.Task, bool, error) {
	normalizedTitle := NormalizeTitle(title)
	if normalizedTitle == "" {
		return db.Task{}, false, nil
	}
	if err := q.LockTaskDuplicateKey(ctx, lockKey(workspaceID, projectID, parentTaskID, normalizedTitle)); err != nil {
		return db.Task{}, false, err
	}
	if allowDuplicate {
		return db.Task{}, false, nil
	}

	duplicate, err := q.FindActiveDuplicateTask(ctx, db.FindActiveDuplicateTaskParams{
		WorkspaceID:     workspaceID,
		ProjectID:       projectID,
		ParentTaskID:   parentTaskID,
		NormalizedTitle: normalizedTitle,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Task{}, false, nil
		}
		return db.Task{}, false, err
	}
	return duplicate, true, nil
}

func LockAndFindRecentAutopilotDuplicate(
	ctx context.Context,
	q *db.Queries,
	workspaceID pgtype.UUID,
	autopilotID pgtype.UUID,
	projectID pgtype.UUID,
	title string,
	window time.Duration,
) (db.Task, bool, error) {
	normalizedTitle := NormalizeTitle(title)
	if normalizedTitle == "" || !autopilotID.Valid || window <= 0 {
		return db.Task{}, false, nil
	}
	if err := q.LockTaskDuplicateKey(ctx, recentAutopilotLockKey(workspaceID, autopilotID, projectID, normalizedTitle)); err != nil {
		return db.Task{}, false, err
	}

	duplicate, err := q.FindRecentAutopilotDuplicateTask(ctx, db.FindRecentAutopilotDuplicateTaskParams{
		WorkspaceID:     workspaceID,
		OriginID:        autopilotID,
		ProjectID:       projectID,
		NormalizedTitle: normalizedTitle,
		CreatedAfter:    pgtype.Timestamptz{Time: time.Now().UTC().Add(-window), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Task{}, false, nil
		}
		return db.Task{}, false, err
	}
	return duplicate, true, nil
}

func lockKey(workspaceID, projectID, parentTaskID pgtype.UUID, normalizedTitle string) string {
	return strings.Join([]string{
		"task-active-duplicate",
		util.UUIDToString(workspaceID),
		util.UUIDToString(projectID),
		util.UUIDToString(parentTaskID),
		normalizedTitle,
	}, "|")
}

func recentAutopilotLockKey(workspaceID, autopilotID, projectID pgtype.UUID, normalizedTitle string) string {
	return strings.Join([]string{
		"autopilot-recent-duplicate",
		util.UUIDToString(workspaceID),
		util.UUIDToString(autopilotID),
		util.UUIDToString(projectID),
		normalizedTitle,
	}, "|")
}
