package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/events"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

// TestRerunTaskPinsForceFreshSessionForRollbackSafety locks in the rollback-safe
// half of the SILI-4869 contract: RerunTask ALWAYS persists
// force_fresh_session=true on the rerun row, no matter how the source task
// failed. The session-reuse decision is made later by the (new) claim handler
// from the source task, so an OLD claim handler picked up mid rolling-deploy —
// which gates the whole resume lookup on !force_fresh_session — degrades to a
// clean start instead of resuming a different execution via the
// (agent, task) most-recent query. The claim-layer behaviour (workdir always
// reused, session gated by source failure class) is asserted in
// TestClaimTask_ManualRetryReusesWorkdir.
func TestRerunTaskPinsForceFreshSessionForRollbackSafety(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, taskID := seedAttributionFixture(t, pool)
	_ = workspaceID

	svc := &AgentRunService{Queries: q, TxStarter: pool, Bus: events.New()}

	// agent_run.runtime_id is NOT NULL; reuse the fixture agent's runtime.
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	// The flag must be true across resume-safe, resume-poisoned, and cancelled
	// source failures alike — the enqueue row never encodes the classification.
	cases := []struct {
		name          string
		status        string
		failureReason any // string, or nil for a NULL failure_reason
	}{
		{name: "transient_timeout", status: "failed", failureReason: "timeout"},
		{name: "transient_provider_network", status: "failed", failureReason: "agent_error.provider_network"},
		{name: "poisoned_context_overflow", status: "failed", failureReason: "agent_error.context_overflow"},
		{name: "cancelled_no_reason", status: "cancelled", failureReason: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sourceID pgtype.UUID
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_run (agent_id, runtime_id, task_id, status, priority, failure_reason, session_id, work_dir)
				VALUES ($1, $2, $3, $4, 0, $5, 'src-session', '/tmp/src-workdir')
				RETURNING id
			`, agentID, runtimeID, taskID, tc.status, tc.failureReason).Scan(&sourceID); err != nil {
				t.Fatalf("insert source run: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, sourceID) })

			agentRun, err := svc.RerunTask(ctx, util.MustParseUUID(taskID), sourceID, pgtype.UUID{}, util.MustParseUUID(creatorID), nil)
			if err != nil {
				t.Fatalf("RerunTask: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_run WHERE id = $1`, agentRun.ID) })

			var forceFresh bool
			var rerunOf pgtype.UUID
			if err := pool.QueryRow(ctx, `
				SELECT force_fresh_session, rerun_of_run_id FROM agent_run WHERE id = $1
			`, agentRun.ID).Scan(&forceFresh, &rerunOf); err != nil {
				t.Fatalf("read rerun run: %v", err)
			}
			if !forceFresh {
				t.Errorf("force_fresh_session = false, want true for rollback safety (source failure_reason=%v)", tc.failureReason)
			}
			if !rerunOf.Valid || rerunOf.Bytes != sourceID.Bytes {
				t.Errorf("rerun_of_run_id = %s, want source %s", util.UUIDToString(rerunOf), util.UUIDToString(sourceID))
			}
		})
	}
}
