package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowHumanDecisionBindsReviewedEvidence(t *testing.T) {
	pool, svc, submit, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, submit); err != nil {
		t.Fatal(err)
	}
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: submit.WorkspaceID, ID: submit.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',jsonb_build_object('type','human','decider',$2::text)) WHERE id=$1`, execution.ID, util.UUIDToString(submit.ActorID)); err != nil {
		t.Fatal(err)
	}
	prepare := func(actor pgtype.UUID) (WorkflowReviewBasis, error) {
		return svc.PrepareHumanReview(ctx, submit.WorkspaceID, submit.InstanceID, submit.ExecutionID, actor)
	}
	if _, err := prepare(submit.ActorID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("incomplete review: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(util.MustParseUUID(uuid.NewString())); !errors.Is(err, ErrWorkflowReviewAccess) {
		t.Fatalf("wrong reviewer: %v", err)
	}
	basis, err := prepare(submit.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	p := RecordWorkflowHumanDecisionParams{WorkspaceID: submit.WorkspaceID, InstanceID: submit.InstanceID, ExecutionID: submit.ExecutionID, ActorID: submit.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash, Outcome: "approved", Note: "Evidence checked"}
	stale := p
	stale.ExpectedResultRevision++
	if _, err := svc.RecordHumanDecision(ctx, stale); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale result approved: %v", err)
	}
	// Concurrent identical requests are one durable decision, even after retries.
	var wg sync.WaitGroup
	ids := make(chan pgtype.UUID, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); d, e := svc.RecordHumanDecision(ctx, p); ids <- d.ID; errs <- e }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first pgtype.UUID
	for id := range ids {
		if first.Valid && id != first {
			t.Fatal("duplicate decision rows")
		}
		first = id
	}
	decision, err := svc.Queries.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.ExecutionID, BasisHash: pgtype.Text{String: basis.Hash, Valid: true}})
	if err != nil || decision.ResultRevision.Int32 != 1 || decision.ExecutionID != execution.ID || len(decision.BasisSnapshot) == 0 {
		t.Fatalf("decision evidence: %+v %v", decision, err)
	}
	opposite := p
	opposite.Outcome = "rejected"
	if _, err := svc.RecordHumanDecision(ctx, opposite); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("rewrote immutable decision: %v", err)
	}
	// A changed completed delivery invalidates the old decision even when the
	// completion counts and result revision remain identical.
	if _, err := pool.Exec(ctx, `UPDATE issue SET title='Revised delivery',updated_at=now() WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	next, err := prepare(submit.ActorID)
	if err != nil || next.Hash == basis.Hash {
		t.Fatalf("changed task reused basis: %s %v", next.Hash, err)
	}
	if _, err := svc.RecordHumanDecision(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale task set approved: %v", err)
	}
	if _, err := svc.Queries.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.ExecutionID, BasisHash: pgtype.Text{String: next.Hash, Valid: true}}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old approval applies to new basis: %v", err)
	}
	p.ExpectedBasisHash = next.Hash
	// A failed ledger insert leaves no decision or execution transition.
	constraint := "review_failure_" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_gate_decision ADD CONSTRAINT %s CHECK (execution_id <> '%s' OR note <> 'injected failure')`, constraint, util.UUIDToString(execution.ID))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "ALTER TABLE workflow_gate_decision DROP CONSTRAINT "+constraint); err != nil {
			t.Error(err)
		}
	})
	p.Note = "injected failure"
	if _, err := svc.RecordHumanDecision(ctx, p); err == nil {
		t.Fatal("expected insertion failure")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_gate_decision WHERE execution_id=$1`, execution.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial decision: %d %v", count, err)
	}
	current, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil || current.Status != "awaiting_review" {
		t.Fatalf("recording changed delivery status: %+v %v", current, err)
	}
	// Human-only approval cannot bypass the first step of a hybrid gate.
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate,type}','"hybrid"') WHERE id=$1`, execution.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(submit.ActorID); !errors.Is(err, ErrWorkflowReviewAccess) {
		t.Fatalf("hybrid bypass: %v", err)
	}
}

func TestWorkflowHumanReviewLocksCallsAndRun(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	execution, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',jsonb_build_object('type','human','decider',$2::text)) WHERE id=$1`, execution.ID, util.UUIDToString(p.ActorID)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM agent_task_queue WHERE id=$1 FOR UPDATE`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if _, err := svc.PrepareHumanReview(short, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID); err == nil {
		t.Fatal("review escaped call lock")
	}
	if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status='cancelled' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareHumanReview(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("cancelled main call accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='completed' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareHumanReview(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("closed Run accepted: %v", err)
	}
}
