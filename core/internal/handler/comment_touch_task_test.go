package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestCreateComment_BumpsTaskUpdatedAt pins SILI-5009: a new comment counts as
// activity on the task, so updated_at advances. This is what lets the
// "Updated date" Kanban/list sort surface recently-discussed cards, not only
// cards whose status changed.
func TestCreateComment_BumpsTaskUpdatedAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	taskID := createCommentTriggerPreviewTask(t, "comment bumps updated_at", "", "")

	var before time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM task WHERE id = $1`, taskID).Scan(&before); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	// Guarantee a measurable wall-clock gap so the bump is unambiguous; now() is
	// evaluated per statement and the task was inserted moments ago.
	time.Sleep(10 * time.Millisecond)

	w := httptest.NewRecorder()
	r := withURLParam(
		newRequest(http.MethodPost, "/api/tasks/"+taskID+"/comments", map[string]any{"content": "a fresh comment"}),
		"id", taskID,
	)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var after time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM task WHERE id = $1`, taskID).Scan(&after); err != nil {
		t.Fatalf("read updated_at after: %v", err)
	}

	if !after.After(before) {
		t.Fatalf("task updated_at was not bumped by a new comment: before=%s after=%s", before, after)
	}
}

// TestCreateComment_WorkspaceMismatchPersistsNothing pins the tenant-integrity
// guarantee of the CreateComment CTE (SILI-5009 nit2): CreateComment is the
// single carrier of "a comment always belongs to an task in the same
// workspace and always bumps it". If the passed workspace does not match the
// target task's workspace, the leading UPDATE matches no task row, the
// dependent INSERT selects nothing, and the :one query returns pgx.ErrNoRows —
// so no mis-attributed comment is written and the task is not touched.
func TestCreateComment_WorkspaceMismatchPersistsNothing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	taskID := createCommentTriggerPreviewTask(t, "comment workspace guard", "", "")

	var before time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM task WHERE id = $1`, taskID).Scan(&before); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	// A workspace that is NOT the task's workspace: the task exists, but the
	// (task, workspace) pair matches no row.
	wrongWorkspace := parseUUID("11111111-1111-1111-1111-111111111111")
	_, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		TaskID:      parseUUID(taskID),
		WorkspaceID: wrongWorkspace,
		AuthorType:  "member",
		AuthorID:    parseUUID(testUserID),
		Content:     "should never persist",
		Type:        "comment",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows on workspace mismatch, got %v", err)
	}

	var commentCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE task_id = $1`, taskID).Scan(&commentCount); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("workspace mismatch must persist no comment, found %d", commentCount)
	}

	var after time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM task WHERE id = $1`, taskID).Scan(&after); err != nil {
		t.Fatalf("read updated_at after: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("workspace mismatch must not bump updated_at: before=%s after=%s", before, after)
	}
}
