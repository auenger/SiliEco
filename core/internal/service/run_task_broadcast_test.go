package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/protocol"
)

// noRowsDBTX makes every read return pgx.ErrNoRows so getTaskPrefix's
// GetWorkspace lookup falls back to an empty prefix without needing a DB. The
// helper under test still publishes regardless of the prefix result.
type noRowsDBTX struct{}

func (noRowsDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}
func (noRowsDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}
func (noRowsDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return noRow{} }

type noRow struct{}

func (noRow) Scan(...any) error { return pgx.ErrNoRows }

// TestBroadcastTaskUpdated_EmitsStatusChange pins the realtime contract behind
// #4648 / SILI-3782: when a background path resets an task's status (e.g. the
// failed-task handler flipping a stuck in_progress task back to todo), it must
// publish task:updated with status_changed=true and the new status so the
// frontend's onTaskUpdated reconcile moves the card between status columns /
// filters instead of leaving it stale until the next unrelated write.
func TestBroadcastTaskUpdated_EmitsStatusChange(t *testing.T) {
	bus := events.New()
	var got []events.Event
	bus.SubscribeAll(func(e events.Event) { got = append(got, e) })

	svc := &AgentRunService{
		Queries: db.New(noRowsDBTX{}),
		Bus:     bus,
	}

	task := db.Task{
		ID:          testUUID(1),
		WorkspaceID: testUUID(2),
		Number:      7,
		Status:      "todo",
	}
	svc.broadcastTaskUpdated(task, "in_progress")

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(got))
	}
	e := got[0]
	if e.Type != protocol.EventTaskUpdated {
		t.Fatalf("expected event type %q, got %q", protocol.EventTaskUpdated, e.Type)
	}
	if e.WorkspaceID != util.UUIDToString(task.WorkspaceID) {
		t.Fatalf("workspace mismatch: got %q want %q", e.WorkspaceID, util.UUIDToString(task.WorkspaceID))
	}

	payload, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", e.Payload)
	}
	if payload["status_changed"] != true {
		t.Errorf("expected status_changed=true, got %v", payload["status_changed"])
	}
	if payload["prev_status"] != "in_progress" {
		t.Errorf("expected prev_status=in_progress, got %v", payload["prev_status"])
	}
	taskMap, ok := payload["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload is not map[string]any: %T", payload["task"])
	}
	if taskMap["status"] != "todo" {
		t.Errorf("expected task.status=todo, got %v", taskMap["status"])
	}
	if taskMap["id"] != util.UUIDToString(task.ID) {
		t.Errorf("task.id mismatch: got %v want %q", taskMap["id"], util.UUIDToString(task.ID))
	}
}

// TestBroadcastTaskUpdated_NoStatusChange guards the gate: a same-status
// broadcast reports status_changed=false so the client skips the status-bucket
// reconcile for non-status field updates.
func TestBroadcastTaskUpdated_NoStatusChange(t *testing.T) {
	bus := events.New()
	var got []events.Event
	bus.SubscribeAll(func(e events.Event) { got = append(got, e) })

	svc := &AgentRunService{
		Queries: db.New(noRowsDBTX{}),
		Bus:     bus,
	}

	task := db.Task{
		ID:          testUUID(1),
		WorkspaceID: testUUID(2),
		Status:      "todo",
	}
	svc.broadcastTaskUpdated(task, "todo")

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(got))
	}
	payload, ok := got[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", got[0].Payload)
	}
	if payload["status_changed"] != false {
		t.Errorf("expected status_changed=false, got %v", payload["status_changed"])
	}
}
