package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestIsDuplicatePendingRunErr locks the driver-code detection (#5914): only a
// 23505 on the idx_one_pending_run_per_task_agent index is a benign
// duplicate-pending-run race. A different unique constraint (e.g. the agent
// name) or a plain error must not be mistaken for it, and the wrapped sentinel
// must remain detectable via errors.Is by callers. This runs without a database.
func TestIsDuplicatePendingRunErr(t *testing.T) {
	dup := &pgconn.PgError{Code: "23505", ConstraintName: "idx_one_pending_run_per_task_agent"}
	if !isDuplicatePendingRunErr(dup) {
		t.Fatal("expected the pending-run unique-index violation to be recognized")
	}
	if isDuplicatePendingRunErr(&pgconn.PgError{Code: "23505", ConstraintName: "agent_workspace_name_unique"}) {
		t.Fatal("a different unique constraint must not be treated as a duplicate pending run")
	}
	if isDuplicatePendingRunErr(errors.New("boom")) {
		t.Fatal("a non-pg error must not be treated as a duplicate pending run")
	}

	wrapped := fmt.Errorf("%w: %v", ErrDuplicatePendingRun, dup)
	if !errors.Is(wrapped, ErrDuplicatePendingRun) {
		t.Fatal("the wrapped sentinel must stay detectable via errors.Is")
	}
}

// TestEnqueueRunForMentionCoalescesDuplicatePendingRun is the service-level
// regression for #5914: a second mention enqueue for the same (task, agent)
// that loses the race to an existing pending run must return the typed
// ErrDuplicatePendingRun sentinel — not a raw create-run error carrying the
// Postgres constraint name — and must leave exactly one pending run behind.
func TestEnqueueRunForMentionCoalescesDuplicatePendingRun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, taskID := seedAttributionFixture(t, pool)
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_run WHERE task_id = $1`, taskID)
	})

	taskStruct := db.Task{
		ID:           util.MustParseUUID(taskID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &AgentRunService{Queries: q, TxStarter: pool, Bus: events.New()}

	// First mention creates the pending run.
	if _, err := svc.EnqueueRunForMention(ctx, taskStruct, util.MustParseUUID(agentID), pgtype.UUID{}); err != nil {
		t.Fatalf("first EnqueueRunForMention: %v", err)
	}

	// Second mention for the same (task, agent) collides on the unique index.
	_, err := svc.EnqueueRunForMention(ctx, taskStruct, util.MustParseUUID(agentID), pgtype.UUID{})
	if !errors.Is(err, ErrDuplicatePendingRun) {
		t.Fatalf("second EnqueueRunForMention: err = %v, want ErrDuplicatePendingRun", err)
	}
	// The returned error must NOT carry the raw Postgres constraint name or
	// SQLSTATE — those used to leak into upper-layer warning logs (#5914).
	for _, leak := range []string{"idx_one_pending_run_per_task_agent", "23505", "SQLSTATE", "duplicate key"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("duplicate error leaked %q: %v", leak, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_run WHERE task_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched')`,
		taskID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending runs: %v", err)
	}
	if n != 1 {
		t.Fatalf("pending run count = %d, want exactly 1", n)
	}
}
