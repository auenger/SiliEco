package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

func TestWorkflowAcceptanceAtomicVersionHandoff(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	first, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	gate := fmt.Sprintf(`{"type":"human","decider":%q}`, util.UUIDToString(p.ActorID))
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET config_snapshot=jsonb_set(config_snapshot,'{gate}',$2::jsonb) WHERE id=$1`, first.ID, gate); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_instance_stage SET gate=$2 WHERE id=$1`, first.WorkflowStageID, gate); err != nil {
		t.Fatal(err)
	}
	var nextStage pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_instance_stage(workspace_id,workflow_instance_id,stable_key,name,position,execution_mode,executor_id,completion_rule,input_spec,output_spec,input_bindings,gate)
 VALUES ($1,$2,'implementation','Implementation',1,'agent',$3,'{"type":"validated_result"}',
 '{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object","required":["prior"],"properties":{"prior":{"type":"object"}}}}}',
 '{"contract":{"schema_version":"silieco.stage/v1","data":{"type":"object"}}}',
 '{"schema_version":"silieco.input-bindings/v1","data":[{"target":"prior","source":{"kind":"stage_result","stage_key":"requirements","pointer":""}}]}',$4) RETURNING id`, p.WorkspaceID, p.InstanceID, first.ExecutorID, gate).Scan(&nextStage); err != nil {
		t.Fatal(err)
	}
	finish := func(execution db.WorkflowStageExecution, submission SubmitWorkflowResultParams) AdvanceWorkflowHumanReviewParams {
		t.Helper()
		if _, err := svc.SubmitResult(ctx, submission); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
			t.Fatal(err)
		}
		basis, err := svc.PrepareHumanReview(ctx, p.WorkspaceID, p.InstanceID, execution.ID, p.ActorID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RecordHumanDecision(ctx, RecordWorkflowHumanDecisionParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, ExecutionID: execution.ID, ActorID: p.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash, Outcome: "approved"}); err != nil {
			t.Fatal(err)
		}
		return AdvanceWorkflowHumanReviewParams{WorkspaceID: p.WorkspaceID, InstanceID: p.InstanceID, ExecutionID: execution.ID, ActorID: p.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash}
	}
	advance := finish(first, p)
	// Force the final outbox insert to fail, after acceptance and Run movement.
	constraint := "acceptance_failure_" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_execution_action ADD CONSTRAINT %s CHECK (workflow_instance_id <> '%s' OR execution_id = '%s' OR kind <> 'dispatch')`, constraint, util.UUIDToString(p.InstanceID), util.UUIDToString(first.ID))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "ALTER TABLE workflow_execution_action DROP CONSTRAINT IF EXISTS "+constraint) })
	if _, err := svc.AdvanceHumanReviewedExecution(ctx, advance); err == nil || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("expected final outbox failure, got %v", err)
	}
	before, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: first.ID})
	if err != nil || before.Status != "awaiting_review" || len(before.Acceptance) != 0 {
		t.Fatalf("partial acceptance: %+v %v", before, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial next execution: %d %v", count, err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_execution_action DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.AdvanceHumanReviewedExecution(ctx, advance)
	if err != nil || updated.CurrentStageID != nextStage || updated.Status != "active" {
		t.Fatalf("advance: %+v %v", updated, err)
	}
	next, err := svc.Queries.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, WorkflowStageID: nextStage})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot workflowcontract.BoundInputSnapshot
	if err := json.Unmarshal(next.InputSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Bindings) != 1 || snapshot.Bindings[0].ExecutionID != util.UUIDToString(first.ID) || snapshot.Bindings[0].ResultRevision != 1 || snapshot.Bindings[0].ResultID == "" || !strings.Contains(string(snapshot.Data), `"prior"`) {
		t.Fatalf("unversioned handoff: %s", next.InputSnapshot)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_execution_action WHERE execution_id=$1 AND kind='dispatch'`, next.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("next dispatch: %d %v", count, err)
	}
	if _, err := svc.AdvanceHumanReviewedExecution(ctx, advance); err != nil {
		t.Fatalf("idempotent advance: %v", err)
	}
	issue, err := svc.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{WorkspaceID: p.WorkspaceID, ID: first.MainIssueID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateIssueStatusWithWorkflowLock(ctx, issue, "todo"); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("accepted source was reopened: %v", err)
	}
	if _, err := svc.MoveIssueStage(ctx, p.WorkspaceID, issue.ID, p.InstanceID, nextStage); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("accepted source moved: %v", err)
	}
	// The same accepted result identity feeds a second execution; completing its
	// own delivery/review completes the Run without creating an extra execution.
	if err := svc.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	next, err = svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: next.ID})
	if err != nil {
		t.Fatal(err)
	}
	lastParams := p
	lastParams.ExecutionID = next.ID
	lastParams.Submission = json.RawMessage(`{"data":{},"artifacts":[]}`)
	lastAdvance := finish(next, lastParams)
	completed, err := svc.AdvanceHumanReviewedExecution(ctx, lastAdvance)
	if err != nil || completed.Status != "completed" || !completed.CompletedAt.Valid {
		t.Fatalf("final acceptance: %+v %v", completed, err)
	}
	if _, err := svc.AdvanceHumanReviewedExecution(ctx, lastAdvance); err != nil {
		t.Fatalf("final retry: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1`, p.InstanceID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate execution: %d %v", count, err)
	}
}

func TestWorkflowAcceptanceRejectsStaleBasisAndSerializesRetries(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	approve := func() AdvanceWorkflowHumanReviewParams {
		t.Helper()
		basis, err := svc.PrepareHumanReview(ctx, submit.WorkspaceID, submit.InstanceID, execution.ID, submit.ActorID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RecordHumanDecision(ctx, RecordWorkflowHumanDecisionParams{WorkspaceID: submit.WorkspaceID, InstanceID: submit.InstanceID, ExecutionID: execution.ID, ActorID: submit.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash, Outcome: "approved"}); err != nil {
			t.Fatal(err)
		}
		return AdvanceWorkflowHumanReviewParams{WorkspaceID: submit.WorkspaceID, InstanceID: submit.InstanceID, ExecutionID: execution.ID, ActorID: submit.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash}
	}
	stale := approve()
	if _, err := pool.Exec(ctx, `UPDATE issue SET title='Changed after approval',updated_at=now() WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AdvanceHumanReviewedExecution(ctx, stale); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("stale approval advanced: %v", err)
	}
	current := approve()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, err := svc.AdvanceHumanReviewedExecution(ctx, current)
			if err == nil && run.Status != "completed" {
				err = fmt.Errorf("unexpected Run status %s", run.Status)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_stage_execution WHERE workflow_instance_id=$1 AND status='succeeded' AND acceptance->>'basis_hash'=$2`, submit.InstanceID, current.ExpectedBasisHash).Scan(&count); err != nil || count != 1 {
		t.Fatalf("acceptance count %d: %v", count, err)
	}
}

func TestWorkflowHumanDecisionAndAcceptanceCommitTogether(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, execution.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, execution.MainTaskID); err != nil {
		t.Fatal(err)
	}
	basis, err := svc.PrepareHumanReview(ctx, submit.WorkspaceID, submit.InstanceID, execution.ID, submit.ActorID)
	if err != nil {
		t.Fatal(err)
	}
	p := RecordWorkflowHumanDecisionParams{WorkspaceID: submit.WorkspaceID, InstanceID: submit.InstanceID, ExecutionID: execution.ID, ActorID: submit.ActorID, ExpectedResultRevision: basis.ResultRevision, ExpectedBasisHash: basis.Hash, Outcome: "approved"}
	constraint := "decision_failure_" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE workflow_instance ADD CONSTRAINT %s CHECK (id <> '%s' OR status <> 'completed')`, constraint, util.UUIDToString(submit.InstanceID))); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(ctx, "ALTER TABLE workflow_instance DROP CONSTRAINT IF EXISTS "+constraint) })
	if _, err := svc.DecideHumanExecution(ctx, p); err == nil || !strings.Contains(err.Error(), constraint) {
		t.Fatalf("expected completion failure: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workflow_gate_decision WHERE execution_id=$1`, execution.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("approval survived rollback: %d %v", count, err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE workflow_instance DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan pgtype.UUID, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := svc.DecideHumanExecution(ctx, p)
			ids <- decision.ID
			errs <- err
		}()
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
		if first.Valid && first != id {
			t.Fatal("duplicate decision")
		}
		first = id
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_instance WHERE id=$1`, submit.InstanceID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("Run not completed: %s %v", status, err)
	}
	p.Note = "Changed retry"
	if _, err := svc.DecideHumanExecution(ctx, p); !errors.Is(err, ErrWorkflowExecutionConflict) {
		t.Fatalf("changed retry accepted: %v", err)
	}
}
