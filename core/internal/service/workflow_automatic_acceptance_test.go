package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowAutomaticAcceptanceRecoveryAndGuards(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	setGate := func(kind string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',jsonb_build_object('type',$2::text)) WHERE id=$1`, e.ID, kind); err != nil {
			t.Fatal(err)
		}
	}
	accept := func() error { return svc.AcceptAutomaticExecution(ctx, p.WorkspaceID, p.InstanceID, p.ExecutionID) }
	setGate("none")
	if err := accept(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("empty delivery accepted: %v", err)
	}
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := accept(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("unfinished work accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
		t.Fatal(err)
	}
	for _, gate := range []string{"human", "agent", "hybrid", ""} {
		setGate(gate)
		if err := accept(); !errors.Is(err, ErrWorkflowReviewAccess) {
			t.Fatalf("%s gate bypass: %v", gate, err)
		}
	}
	setGate("none")
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='waiting' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if err := accept(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("paused Run accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance SET status='active' WHERE id=$1`, p.InstanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='cancelled' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if err := accept(); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("cancelled requirement accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	// Fresh service instances represent independent workers after a restart.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := WorkflowExecutionService{Queries: svc.Queries, TxStarter: svc.TxStarter}
			errs <- worker.ScanAutomaticAcceptance(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var status, step string
	if err := pool.QueryRow(ctx, `SELECT r.status,e.acceptance->>'review_step' FROM workflow_instance r JOIN workflow_stage_execution e ON e.workflow_instance_id=r.id WHERE e.id=$1`, e.ID).Scan(&status, &step); err != nil || status != "completed" || step != "none" {
		t.Fatalf("acceptance: %s %s %v", status, step, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_gate_decision WHERE execution_id=$1`, e.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("fabricated review: %d %v", count, err)
	}
	if err := svc.ScanAutomaticAcceptance(ctx); err != nil {
		t.Fatal(err)
	}
}
