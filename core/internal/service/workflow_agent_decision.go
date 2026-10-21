package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
)

type RecordWorkflowAgentDecisionParams struct {
	WorkspaceID            pgtype.UUID
	RequestID              pgtype.UUID
	ActorID                pgtype.UUID
	TaskID                 pgtype.UUID
	ExpectedResultRevision int32
	ExpectedBasisHash      string
	Outcome                string
	Note                   string
}

// RecordAgentDecision records evidence-bound review only. A running reviewer
// cannot advance the stage; acceptance additionally requires its call to finish.
func (s *WorkflowExecutionService) RecordAgentDecision(ctx context.Context, p RecordWorkflowAgentDecisionParams) (db.WorkflowGateDecision, error) {
	empty := db.WorkflowGateDecision{}
	if !p.ActorID.Valid || !p.TaskID.Valid || p.ExpectedResultRevision < 1 || len(p.ExpectedBasisHash) != 64 || (p.Outcome != "approved" && p.Outcome != "rejected") {
		return empty, ErrWorkflowReviewAccess
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	observed, err := q.GetWorkflowReviewRequest(ctx, db.GetWorkflowReviewRequestParams{WorkspaceID: p.WorkspaceID, ID: p.RequestID})
	if err != nil {
		return empty, err
	}
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, observed.WorkflowInstanceID)
	if err != nil {
		return empty, err
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: observed.ExecutionID})
	if err != nil {
		return empty, err
	}
	if execution.Status == "succeeded" {
		decision, err := s.replayAcceptedAgentDecision(ctx, q, run, execution, p)
		if err != nil {
			return empty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return decision, nil
	}
	request, err := s.prepareAgentReviewInTx(ctx, q, p.WorkspaceID, observed.WorkflowInstanceID, observed.ExecutionID)
	if err != nil {
		return empty, err
	}
	if request.ID != p.RequestID || request.BasisHash != p.ExpectedBasisHash || request.ResultRevision != p.ExpectedResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	if request.ReviewerID != p.ActorID || request.TaskID != p.TaskID || (request.Status != "queued" && request.Status != "decided") {
		return empty, ErrWorkflowReviewAccess
	}
	task, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: p.WorkspaceID, ID: p.TaskID})
	if err != nil {
		return empty, err
	}
	if task.AgentID != p.ActorID || task.WorkflowReviewRequestID != request.ID || task.WorkflowStageExecutionID.Valid || (task.Status != "running" && task.Status != "completed") {
		return empty, ErrWorkflowReviewAccess
	}
	existing, err := q.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: request.ExecutionID, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, ReviewTaskID: request.TaskID})
	if err == nil {
		if existing.ActorID != p.ActorID || existing.Outcome != p.Outcome || existing.Note.String != p.Note {
			return empty, ErrWorkflowExecutionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: request.ExecutionID})
	if err != nil {
		return empty, err
	}
	decision, err := q.CreateWorkflowAgentDecision(ctx, db.CreateWorkflowAgentDecisionParams{WorkspaceID: p.WorkspaceID, WorkflowInstanceID: request.WorkflowInstanceID, FromStageID: e.WorkflowStageID, Outcome: p.Outcome, ActorID: p.ActorID, Note: pgtype.Text{String: p.Note, Valid: p.Note != ""}, ExecutionID: e.ID, ResultRevision: pgtype.Int4{Int32: request.ResultRevision, Valid: true}, BasisHash: pgtype.Text{String: request.BasisHash, Valid: true}, BasisSnapshot: request.BasisSnapshot, ReviewTaskID: request.TaskID})
	if err != nil {
		return empty, err
	}
	if _, err := q.MarkWorkflowReviewDecided(ctx, db.MarkWorkflowReviewDecidedParams{WorkspaceID: p.WorkspaceID, ID: request.ID, TaskID: p.TaskID}); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return decision, nil
}

// completedAgentApproval is checked again for hybrid human review and eventual
// automatic acceptance. An approval from an unfinished/failed call is insufficient.
func (s *WorkflowExecutionService) completedAgentApproval(ctx context.Context, q *db.Queries, run db.WorkflowInstance, e db.WorkflowStageExecution, basis WorkflowReviewBasis) (db.WorkflowGateDecision, error) {
	empty := db.WorkflowGateDecision{}
	request, err := s.prepareAgentReviewInTx(ctx, q, run.WorkspaceID, run.ID, e.ID)
	if err != nil {
		return empty, err
	}
	if request.Status != "decided" || request.BasisHash != basis.Hash || request.ResultRevision != basis.ResultRevision || !request.TaskID.Valid {
		return empty, ErrWorkflowExecutionConflict
	}
	task, err := q.GetWorkflowResultTask(ctx, db.GetWorkflowResultTaskParams{WorkspaceID: run.WorkspaceID, ID: request.TaskID})
	if err != nil {
		return empty, err
	}
	if task.Status != "completed" || task.AgentID != request.ReviewerID || task.WorkflowReviewRequestID != request.ID {
		return empty, ErrWorkflowExecutionConflict
	}
	decision, err := q.GetWorkflowAgentDecisionByBasis(ctx, db.GetWorkflowAgentDecisionByBasisParams{WorkspaceID: run.WorkspaceID, ExecutionID: e.ID, BasisHash: pgtype.Text{String: basis.Hash, Valid: true}, ReviewTaskID: request.TaskID})
	if err != nil {
		return empty, err
	}
	if decision.Outcome != "approved" || decision.ActorID != request.ReviewerID || decision.ResultRevision.Int32 != basis.ResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	return decision, nil
}

// AcceptAgentReviewedExecution advances only after the bound reviewer completed
// successfully and approved the same still-current evidence. Hybrid uses the
// human decision transaction instead of this entry point.
func (s *WorkflowExecutionService) AcceptAgentReviewedExecution(ctx context.Context, workspaceID, instanceID, executionID pgtype.UUID) (db.WorkflowInstance, error) {
	empty := db.WorkflowInstance{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, workspaceID, instanceID)
	if err != nil {
		return empty, err
	}
	e, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: workspaceID, ID: executionID})
	if err != nil {
		return empty, err
	}
	if e.WorkflowInstanceID != instanceID {
		return empty, ErrWorkflowExecutionConflict
	}
	var config struct {
		Gate struct {
			Type string `json:"type"`
		} `json:"gate"`
	}
	if err := json.Unmarshal(e.ConfigSnapshot, &config); err != nil {
		return empty, err
	}
	if config.Gate.Type != "agent" {
		return empty, ErrWorkflowReviewAccess
	}
	if e.Status == "succeeded" {
		var acceptance workflowAcceptance
		if json.Unmarshal(e.Acceptance, &acceptance) != nil || acceptance.ReviewStep != "agent" || acceptance.SchemaVersion != "silieco.acceptance/v1" {
			return empty, ErrWorkflowExecutionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return run, nil
	}
	basis, err := s.captureWorkflowReviewBasis(ctx, q, run, e, false)
	if err != nil {
		return empty, err
	}
	decision, err := s.completedAgentApproval(ctx, q, run, e, basis)
	if err != nil {
		return empty, err
	}
	updated, err := s.advanceAcceptedExecutionInTx(ctx, q, run, basis, decision.ID, "agent")
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	s.publishAcceptedWorkflow(workspaceID, instanceID)
	return updated, nil
}
