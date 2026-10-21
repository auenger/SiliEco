package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/silieco-ai/silieco/core/internal/util"
	db "github.com/silieco-ai/silieco/core/pkg/db/generated"
	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
)

type workflowAcceptance struct {
	SchemaVersion  string          `json:"schema_version"`
	ResultID       pgtype.UUID     `json:"result_id"`
	ResultRevision int32           `json:"result_revision"`
	BasisHash      string          `json:"basis_hash"`
	BasisSnapshot  json.RawMessage `json:"basis_snapshot"`
	ReviewStep     string          `json:"review_step"`
	DecisionID     pgtype.UUID     `json:"decision_id"`
	AcceptedAt     time.Time       `json:"accepted_at"`
}

type AdvanceWorkflowHumanReviewParams struct {
	WorkspaceID            pgtype.UUID
	InstanceID             pgtype.UUID
	ExecutionID            pgtype.UUID
	ActorID                pgtype.UUID
	ExpectedResultRevision int32
	ExpectedBasisHash      string
}

// AdvanceHumanReviewedExecution rechecks the recorded review and atomically
// accepts its exact result, moves the Run, and establishes the next execution's
// frozen inputs plus durable dispatch intent. An identical retry is idempotent.
// The API uses DecideHumanExecution to combine decision creation with acceptance.
func (s *WorkflowExecutionService) AdvanceHumanReviewedExecution(ctx context.Context, p AdvanceWorkflowHumanReviewParams) (db.WorkflowInstance, error) {
	empty := db.WorkflowInstance{}
	if !p.WorkspaceID.Valid || !p.InstanceID.Valid || !p.ExecutionID.Valid || !p.ActorID.Valid || p.ExpectedResultRevision < 1 || len(p.ExpectedBasisHash) != 64 {
		return empty, ErrWorkflowExecutionConflict
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	run, err := lockWorkflowExecutionRun(ctx, q, p.WorkspaceID, p.InstanceID)
	if err != nil {
		return empty, err
	}
	if _, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{WorkspaceID: p.WorkspaceID, UserID: p.ActorID}); err != nil {
		return empty, ErrWorkflowReviewAccess
	}
	execution, err := q.GetWorkflowStageExecution(ctx, db.GetWorkflowStageExecutionParams{WorkspaceID: p.WorkspaceID, ID: p.ExecutionID})
	if err != nil {
		return empty, err
	}
	if execution.WorkflowInstanceID != p.InstanceID {
		return empty, pgx.ErrNoRows
	}
	decision, err := q.GetWorkflowExecutionDecisionByBasis(ctx, db.GetWorkflowExecutionDecisionByBasisParams{WorkspaceID: p.WorkspaceID, ExecutionID: p.ExecutionID, BasisHash: pgtype.Text{String: p.ExpectedBasisHash, Valid: true}})
	if err != nil {
		return empty, err
	}
	if decision.ActorID != p.ActorID || decision.Outcome != "approved" || decision.ResultRevision.Int32 != p.ExpectedResultRevision {
		return empty, ErrWorkflowReviewAccess
	}
	if execution.Status == "succeeded" {
		var accepted workflowAcceptance
		if json.Unmarshal(execution.Acceptance, &accepted) != nil || accepted.SchemaVersion != "silieco.acceptance/v1" || accepted.DecisionID != decision.ID || accepted.ResultRevision != p.ExpectedResultRevision || accepted.BasisHash != p.ExpectedBasisHash {
			return empty, ErrWorkflowExecutionConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return empty, err
		}
		return run, nil
	}
	basis, err := s.lockHumanReviewBasis(ctx, q, p.WorkspaceID, p.InstanceID, p.ExecutionID, p.ActorID)
	if err != nil {
		return empty, err
	}
	if basis.Hash != p.ExpectedBasisHash || basis.ResultRevision != p.ExpectedResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	updated, err := s.advanceAcceptedExecutionInTx(ctx, q, run, basis, decision.ID, "human")
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}
	return updated, nil
}

func (s *WorkflowExecutionService) advanceAcceptedExecutionInTx(ctx context.Context, q *db.Queries, run db.WorkflowInstance, basis WorkflowReviewBasis, decisionID pgtype.UUID, reviewStep string) (db.WorkflowInstance, error) {
	empty := db.WorkflowInstance{}
	execution := basis.execution
	result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: run.WorkspaceID, ExecutionID: execution.ID, Revision: basis.ResultRevision})
	if err != nil {
		return empty, err
	}
	accepted, err := json.Marshal(workflowAcceptance{SchemaVersion: "silieco.acceptance/v1", ResultID: result.ID, ResultRevision: basis.ResultRevision, BasisHash: basis.Hash, BasisSnapshot: basis.Snapshot, ReviewStep: reviewStep, DecisionID: decisionID, AcceptedAt: time.Now().UTC()})
	if err != nil {
		return empty, err
	}
	if _, err := q.AcceptWorkflowStageExecution(ctx, db.AcceptWorkflowStageExecutionParams{WorkspaceID: run.WorkspaceID, ID: execution.ID, Acceptance: accepted, ResultRevision: basis.ResultRevision}); err != nil {
		return empty, err
	}
	current, err := q.GetWorkflowInstanceStage(ctx, db.GetWorkflowInstanceStageParams{ID: execution.WorkflowStageID, WorkspaceID: run.WorkspaceID, WorkflowInstanceID: run.ID})
	if err != nil {
		return empty, err
	}
	next, err := q.GetNextWorkflowInstanceStage(ctx, db.GetNextWorkflowInstanceStageParams{WorkflowInstanceID: run.ID, WorkspaceID: run.WorkspaceID, Position: current.Position})
	status := "active"
	stageID := next.ID
	if errors.Is(err, pgx.ErrNoRows) {
		status = "completed"
		stageID = current.ID
	} else if err != nil {
		return empty, err
	}
	updated, err := q.UpdateWorkflowInstanceState(ctx, db.UpdateWorkflowInstanceStateParams{ID: run.ID, WorkspaceID: run.WorkspaceID, Status: pgtype.Text{String: status, Valid: true}, CurrentStageID: stageID})
	if err != nil {
		return empty, err
	}
	if status == "active" && next.ExecutionMode == "agent" {
		if _, err := s.EnsureStageExecutionInTx(ctx, q, EnsureWorkflowExecutionParams{WorkspaceID: run.WorkspaceID, InstanceID: run.ID, StageID: next.ID, ExpectedRevision: run.Revision, ExecutorID: next.ExecutorID, InputSnapshot: run.Inputs}); err != nil {
			return empty, err
		}
	}
	return updated, nil
}

func (s *WorkflowExecutionService) resolveAcceptedInputResult(ctx context.Context, q *db.Queries, run db.WorkflowInstance, target db.WorkflowInstanceStage, stageKey string) (workflowcontract.AcceptedInputResult, error) {
	empty := workflowcontract.AcceptedInputResult{}
	source, err := q.GetWorkflowInstanceStageByStableKey(ctx, db.GetWorkflowInstanceStageByStableKeyParams{WorkflowInstanceID: run.ID, WorkspaceID: run.WorkspaceID, StableKey: stageKey})
	if err != nil {
		return empty, err
	}
	if source.Position >= target.Position {
		return empty, ErrWorkflowExecutionConflict
	}
	execution, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: run.WorkspaceID, WorkflowInstanceID: run.ID, WorkflowStageID: source.ID})
	if err != nil {
		return empty, err
	}
	var accepted workflowAcceptance
	if execution.Status != "succeeded" || json.Unmarshal(execution.Acceptance, &accepted) != nil || accepted.SchemaVersion != "silieco.acceptance/v1" || accepted.ResultRevision < 1 || accepted.ResultRevision != execution.ResultRevision {
		return empty, ErrWorkflowExecutionConflict
	}
	basis, err := s.captureWorkflowReviewBasis(ctx, q, run, execution, true)
	if err != nil {
		return empty, err
	}
	if basis.Hash != accepted.BasisHash {
		return empty, ErrWorkflowExecutionConflict
	}
	result, err := q.GetWorkflowExecutionResult(ctx, db.GetWorkflowExecutionResultParams{WorkspaceID: run.WorkspaceID, ExecutionID: execution.ID, Revision: accepted.ResultRevision})
	if err != nil {
		return empty, err
	}
	if result.ID != accepted.ResultID {
		return empty, ErrWorkflowExecutionConflict
	}
	return workflowcontract.AcceptedInputResult{StageKey: stageKey, ExecutionID: util.UUIDToString(execution.ID), ResultID: util.UUIDToString(result.ID), Revision: result.Revision, Data: result.Data}, nil
}

// Ordinary edits cannot rewrite accepted delivery. Explicit rework will create
// a new business attempt before allowing source-stage mutations again.
func ensureWorkflowStageMutable(ctx context.Context, q *db.Queries, workspaceID, instanceID, stageID pgtype.UUID) error {
	if !instanceID.Valid || !stageID.Valid {
		return nil
	}
	execution, err := q.GetLatestWorkflowStageExecution(ctx, db.GetLatestWorkflowStageExecutionParams{WorkspaceID: workspaceID, WorkflowInstanceID: instanceID, WorkflowStageID: stageID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if execution.Status == "succeeded" || execution.FailureCode.String == "rework_invalidated" {
		return ErrWorkflowExecutionConflict
	}
	return nil
}
