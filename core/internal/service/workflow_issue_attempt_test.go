package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

func TestWorkflowIssueAttemptScopesReviewEvidence(t *testing.T) {
	pool, svc, p, _ := workflowResultFixture(t)
	ctx := context.Background()
	if _, err := svc.SubmitResult(ctx, p); err != nil {
		t.Fatal(err)
	}
	e, err := svc.Queries.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		t.Fatal(err)
	}
	main, err := svc.Queries.GetIssue(ctx, e.MainIssueID)
	if err != nil || main.WorkflowStageExecutionID != e.ID {
		t.Fatalf("dispatch issue not bound: %+v %v", main, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workflow_stage_execution SET attempt=2 WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	var oldID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workflow_stage_execution(workspace_id,workflow_instance_id,workflow_stage_id,attempt,plan_revision,executor_id,config_snapshot,input_snapshot,status) SELECT workspace_id,workflow_instance_id,workflow_stage_id,1,plan_revision,executor_id,config_snapshot,input_snapshot,'cancelled' FROM workflow_stage_execution WHERE id=$1 RETURNING id`, e.ID).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	old, err := svc.Queries.CreateIssue(ctx, db.CreateIssueParams{WorkspaceID: p.WorkspaceID, Title: "Old incomplete delivery", Status: "todo", Priority: "none", CreatorType: "member", CreatorID: p.ActorID, Number: 999, WorkflowInstanceID: p.InstanceID, WorkflowStageID: e.WorkflowStageID})
	if err != nil {
		t.Fatal(err)
	}
	if old.WorkflowStageExecutionID != e.ID {
		t.Fatal("new issue did not bind latest business attempt")
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET workflow_stage_execution_id=$2 WHERE id=$1`, old.ID, oldID); err != nil {
		t.Fatal(err)
	}
	agent, err := svc.Queries.GetAgent(ctx, e.ExecutorID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: e.ExecutorID, RuntimeID: agent.RuntimeID, IssueID: old.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='done' WHERE id=$1`, e.MainIssueID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, e.MainTaskID); err != nil {
		t.Fatal(err)
	}
	capture := func() WorkflowReviewBasis {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		q := svc.Queries.WithTx(tx)
		run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
		if err != nil {
			t.Fatal(err)
		}
		current, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: e.ID})
		if err != nil {
			t.Fatal(err)
		}
		basis, err := svc.captureWorkflowReviewBasis(ctx, q, run, current, false)
		if err != nil {
			t.Fatal(err)
		}
		return basis
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, mutationErr := LockIssueWorkflowMutation(ctx, svc.Queries.WithTx(tx), p.WorkspaceID, old.ID, pgtype.UUID{})
	_ = tx.Rollback(ctx)
	if !errors.Is(mutationErr, ErrWorkflowExecutionConflict) {
		t.Fatalf("old business attempt mutable: %v", mutationErr)
	}
	basis := capture()
	if len(basis.Issues) != 1 || basis.Issues[0].ID != e.MainIssueID || len(basis.Calls) != 1 {
		t.Fatalf("old attempt leaked: %+v", basis)
	}
	var snapshot struct {
		Issues    []json.RawMessage `json:"issues"`
		Execution json.RawMessage   `json:"execution"`
	}
	if err := json.Unmarshal(basis.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(snapshot.Execution), "rework_source_execution_id") || strings.Contains(string(snapshot.Execution), "rework_metadata") {
		t.Fatal("v3 execution projection changed")
	}
	if strings.Contains(string(snapshot.Issues[0]), "workflow_stage_execution_id") || strings.Contains(string(snapshot.Issues[0]), "workflow_invalidated_by_rework_id") {
		t.Fatal("v3 issue projection changed")
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET title='Old delivery updated',updated_at=now() WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if refreshed := capture(); refreshed.Hash != basis.Hash {
		t.Fatal("old attempt changed current evidence")
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET workflow_stage_execution_id=NULL WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	readiness, err := svc.Queries.GetWorkflowExecutionReadiness(ctx, db.GetWorkflowExecutionReadinessParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: p.InstanceID, ID: e.ID})
	if err != nil || readiness.IncompleteIssues != 1 || readiness.ActiveCalls != 1 {
		t.Fatalf("legacy unbound work bypassed: %+v %v", readiness, err)
	}
}
